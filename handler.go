// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package coronanet

import (
	"encoding/gob"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/coronanet/go-coronanet/protocol/corona"
	"github.com/coronanet/go-coronanet/protocol/system"
	"github.com/coronanet/go-coronanet/tornet"
	"github.com/ethereum/go-ethereum/log"
	"golang.org/x/crypto/sha3"
)

// coronaMessage is an envelope containing all possible messages received through
// the Corona Network wire protocol.
type coronaMessage struct {
	Handshake  *system.Handshake
	Disconnect *system.Disconnect
	GetProfile *corona.GetProfile
	Profile    *corona.Profile
	GetAvatar  *corona.GetAvatar
	Avatar     *corona.Avatar
}

// handleContact is ran when a remote contact connects to us via the tornet.
func (b *Backend) handleContact(uid tornet.IdentityFingerprint, conn net.Conn) {
	// Create a logger to track what's going on
	logger := log.New("contact", uid)
	logger.Info("Contact connected")

	// Create the gob encoder and decoder
	enc := gob.NewEncoder(conn)
	dec := gob.NewDecoder(conn)

	// Run the protocol handshake and catch any errors. Since we're not yet in
	// the separate reader/writer phase, we can't send over errors. Just nuke
	// the connection.
	ver, err := b.handleContactHandshake(enc, dec)
	if err != nil {
		logger.Warn("Protocol handshake failed", "err", err)
		return
	}
	// Common protocol version negotiated, start up the actual message handler
	switch ver {
	case 1:
		// Version one will do a profile exchange on connect
		go enc.Encode(&coronaMessage{GetProfile: &corona.GetProfile{}})
		err := b.handleContactV1(logger, uid, enc, dec)
		if err != nil {
			// Something failed horribly, try to send over an error
			conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
			enc.Encode(&coronaMessage{Disconnect: &system.Disconnect{Reason: err.Error()}})
		}
		logger.Warn("Connection torn down", "err", err)
		return
	default:
		panic(fmt.Sprintf("unhandled corona protocol version: %d", ver))
	}
}

// handleContactHandshake runs the `corona` protocol negotiation and returns the
// common version number agreed upon.
func (b *Backend) handleContactHandshake(enc *gob.Encoder, dec *gob.Decoder) (uint, error) {
	// All protocols start with a system handshake, send ours, read theirs
	errc := make(chan error, 2)
	go func() {
		errc <- enc.Encode(&coronaMessage{
			Handshake: &system.Handshake{Protocol: corona.Protocol, Versions: []uint{1}},
		})
	}()
	message := new(coronaMessage)
	go func() {
		errc <- dec.Decode(message)
	}()
	timeout := time.NewTimer(3 * time.Second)
	defer timeout.Stop()
	for i := 0; i < 2; i++ {
		select {
		case err := <-errc:
			if err != nil {
				return 0, err
			}
		case <-timeout.C:
			return 0, errors.New("handshake timed out")
		}
	}
	// Find the common protocol, abort otherwise
	if message.Handshake == nil {
		return 0, fmt.Errorf("handshake missing from first message")
	}
	handshake := message.Handshake
	if handshake.Protocol != corona.Protocol {
		return 0, fmt.Errorf("unexpected corona protocol: %s", handshake.Protocol)
	}
	var version uint
	for _, v := range handshake.Versions {
		if v == 1 { // Bit forced with only 1 supported version, should be better later
			version = 1
			break
		}
	}
	if version == 0 {
		return 0, fmt.Errorf("no common protocol version: %v vs %v", []uint{1}, handshake.Protocol)
	}
	return version, nil
}

// handleContactV1 is ran when a remote contact connects to us via the tornet
// and negotiates a common `corona` protocol version of 1.
func (b *Backend) handleContactV1(logger log.Logger, uid tornet.IdentityFingerprint, enc *gob.Encoder, dec *gob.Decoder) error {
	// Track the peer while connected to allow sending direct updates too
	b.lock.Lock()
	if _, ok := b.peerset[uid]; ok {
		panic("peer already registered")
	}
	b.peerset[uid] = enc
	b.lock.Unlock()

	defer func() {
		b.lock.Lock()
		delete(b.peerset, uid)
		b.lock.Unlock()
	}()

	// Start processing messages until torn down
	for {
		// Read the next message off the network
		message := new(coronaMessage)
		if err := dec.Decode(message); err != nil {
			return err
		}
		// Depending on what we've got, do something meaningful
		switch {
		case message.Handshake != nil:
			logger.Warn("Contact sent double handshake")
			return errors.New("unexpected handshake")

		case message.Disconnect != nil:
			if message.Disconnect.Reason != "" {
				logger.Warn("Contact dropped connection", "reason", message.Disconnect.Reason)
			}
			return nil

		case message.GetProfile != nil:
			logger.Info("Contact requested profile")
			prof, err := b.Profile()
			if err != nil {
				panic(err) // Profile must exist for networking
			}
			if err := enc.Encode(&coronaMessage{Profile: &corona.Profile{
				Name:   prof.Name,
				Avatar: prof.Avatar,
			}}); err != nil {
				return err
			}

		case message.Profile != nil:
			logger.Info("Contact sent profile", "name", message.Profile.Name, "avatar", hex.EncodeToString(message.Profile.Avatar[:]))

			// Update the profile name if initial exchange, ignore otherwise
			info, err := b.Contact(uid)
			if err != nil {
				panic(err) // Profile must exist for this handler to run
			}
			if info.Name == "" {
				logger.Info("Setting initial name")
				if err := b.UpdateContact(uid, message.Profile.Name); err != nil {
					// Well, shit. Not much we can do, ignore and run with it
					logger.Warn("Failed to set initial name", "err", err)
				}
			} else if info.Name != message.Profile.Name {
				logger.Warn("Rejecting remote name change", "have", info.Name)
			}
			// If the avatar was changed, request te new one
			if info.Avatar != message.Profile.Avatar {
				go enc.Encode(&coronaMessage{GetAvatar: &corona.GetAvatar{}})
			}

		case message.GetAvatar != nil:
			logger.Info("Contact requested avatar")
			prof, err := b.Profile()
			if err != nil {
				panic(err) // Profile must exist for networking
			}
			if prof.Avatar == ([32]byte{}) {
				// No avatar set, sorry
				logger.Info("No avatar to send over", "err", err)
				go enc.Encode(&coronaMessage{Avatar: &corona.Avatar{Image: []byte{}}})
				continue
			}
			img, err := b.CDNImage(prof.Avatar)
			if err != nil {
				// Something funky happened, warn and nuke the remote image
				logger.Warn("Local avatar unavailable", "err", err)
				go enc.Encode(&coronaMessage{Avatar: &corona.Avatar{Image: []byte{}}})
				continue
			}
			if err := enc.Encode(&coronaMessage{Avatar: &corona.Avatar{Image: img}}); err != nil {
				return err
			}

		case message.Avatar != nil:
			// If the remote user deleted their avatar, delete locally too
			if len(message.Avatar.Image) == 0 {
				logger.Info("Contact deleted their avatar")
				if err := b.deleteContactPicture(uid); err != nil {
					logger.Warn("Failed to delete avatar", "err", err)
				}
				return nil
			}
			// Remote user sent new avatar, inject it into the database
			hash := sha3.Sum256(message.Avatar.Image)

			logger.Info("Contact sent avatar", "hash", hex.EncodeToString(hash[:]), "bytes", len(message.Avatar.Image))
			if err := b.uploadContactPicture(uid, message.Avatar.Image); err != nil {
				logger.Warn("Failed to set avatar", "err", err)
			}
		}
	}
	return nil
}
