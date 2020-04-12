// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package coronanet

import (
	"encoding/gob"
	"encoding/hex"
	"net"
	"time"

	"github.com/coronanet/go-coronanet/protocols"
	"github.com/coronanet/go-coronanet/protocols/corona"
	"github.com/coronanet/go-coronanet/tornet"
	"github.com/ethereum/go-ethereum/log"
	"golang.org/x/crypto/sha3"
)

// handleContactV1 is ran when a remote contact connects to us via the `tornet`
// and negotiates a common `corona` protocol version of 1.
func (b *Backend) handleContactV1(uid tornet.IdentityFingerprint, conn net.Conn, enc *gob.Encoder, dec *gob.Decoder, logger log.Logger) {
	err := b.handleContactV1Internal(uid, enc, dec, logger)
	if err != nil {
		// Something failed horribly, try to send over an error
		conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
		enc.Encode(&corona.Envelope{Disconnect: &protocols.Disconnect{Reason: err.Error()}})
	}
	logger.Warn("Connection torn down", "err", err)
}

// handleContactV1Internal is ran when a remote contact connects to us via the tornet
// and negotiates a common `corona` protocol version of 1.
func (b *Backend) handleContactV1Internal(uid tornet.IdentityFingerprint, enc *gob.Encoder, dec *gob.Decoder, logger log.Logger) error {
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

	// Version one will do a profile exchange on connect
	go enc.Encode(&corona.Envelope{GetProfile: &corona.GetProfile{}})

	// Start processing messages until torn down
	for {
		// Read the next message off the network
		message := new(corona.Envelope)
		if err := dec.Decode(message); err != nil {
			return err
		}
		// Depending on what we've got, do something meaningful
		switch {
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
			if err := enc.Encode(&corona.Envelope{Profile: &corona.Profile{
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
				go enc.Encode(&corona.Envelope{GetAvatar: &corona.GetAvatar{}})
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
				go enc.Encode(&corona.Envelope{Avatar: &corona.Avatar{Image: []byte{}}})
				continue
			}
			img, err := b.CDNImage(prof.Avatar)
			if err != nil {
				// Something funky happened, warn and nuke the remote image
				logger.Warn("Local avatar unavailable", "err", err)
				go enc.Encode(&corona.Envelope{Avatar: &corona.Avatar{Image: []byte{}}})
				continue
			}
			if err := enc.Encode(&corona.Envelope{Avatar: &corona.Avatar{Image: img}}); err != nil {
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
