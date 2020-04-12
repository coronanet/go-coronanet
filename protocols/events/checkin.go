// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package events

import (
	"context"
	"crypto/ed25519"
	"encoding/gob"
	"errors"
	"net"
	"time"

	"github.com/coronanet/go-coronanet/tornet"
	"github.com/ethereum/go-ethereum/log"
)

// CheckinSession is a temporary pairing session where an external user can be
// invited to become a trusted participant of an event.
type CheckinSession struct {
	Identity tornet.PublicIdentity // Public identity of the server to check in to
	Address  tornet.PublicAddress  // Public address of the server to check in to
	Auth     tornet.SecretIdentity // Ephemeral authentication credential

	server *Server    // Event server to check into
	result chan error // Checkin result for user feedback
}

// Checkin starts a new checkin session. Normally you don't want to support more
// than one concurrent checkin, but it might come useful later on.
func (s *Server) Checkin() (*CheckinSession, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.infos.End != (time.Time{}) {
		return nil, ErrEventConcluded
	}
	auth, err := tornet.GenerateIdentity()
	if err != nil {
		return nil, err
	}
	session := &CheckinSession{
		Identity: s.infos.Identity.Public(),
		Address:  s.infos.Address.Public(),
		Auth:     auth,
		server:   s,
		result:   make(chan error, 3), // Checkin && end event && wait defer
	}
	s.checkins[auth.Fingerprint()] = session
	s.peerset.Trust(auth.Public())
	return session, nil
}

// close cleans up the checkin session from the event server.
//
// Note, this method assumes the server lock is held.
func (cs *CheckinSession) close() {
	cs.server.peerset.Untrust(cs.Auth.Fingerprint())
	delete(cs.server.checkins, cs.Auth.Fingerprint())
	cs.result <- errors.New("session closed")
}

// Wait blocks until the checkin session concludes or the context is cancelled.
func (cs *CheckinSession) Wait(ctx context.Context) error {
	// Once wait terminates, the checkin session should be removed from the event
	// server. It might have already been partially removed by a successful round
	// or the event being ended.
	defer func() {
		cs.server.lock.Lock()
		defer cs.server.lock.Unlock()

		cs.close()
	}()
	// Wait for the session to succeed, fail or time out
	select {
	case <-ctx.Done():
		return errors.New("context cancelled")
	case err := <-cs.result:
		return err
	}
}

// handleV1CheckIn is the network handler for the v1 `event` protocol's checkin
// phase.
func (s *Server) handleV1CheckIn(uid tornet.IdentityFingerprint, conn net.Conn, enc *gob.Encoder, dec *gob.Decoder, logger log.Logger) error {
	logger.Info("Participant checking in")

	// The entire exchange is time limited, ensure failure if it's exceeded
	conn.SetDeadline(time.Now().Add(checkinTimeout))

	// Read the checkin request and validate the digital signature
	message := new(Envelope)
	if err := dec.Decode(message); err != nil {
		logger.Warn("Checkin retrieval failed", "err", err)
		return err
	}
	if message.Checkin == nil {
		logger.Warn("Checkin message missing")
		return errors.New("checkin message missing")
	}
	if len(message.Checkin.Pseudonym) != ed25519.PublicKeySize {
		logger.Warn("Invalid checkin identity length", "bytes", len(message.Checkin.Pseudonym))
		return errors.New("invalid checkin identity length")
	}
	if len(message.Checkin.Signature) != ed25519.SignatureSize {
		logger.Warn("Invalid checkin signature length", "bytes", len(message.Checkin.Signature))
		return errors.New("invalid checkin signature length")
	}
	if !message.Checkin.Pseudonym.Verify(s.infos.Identity.Public(), message.Checkin.Signature) {
		logger.Warn("Invalid checkin signature")
		return errors.New("invalid checkin signature")
	}
	// Checkin completed, authorize the identity to connect for data exchange
	uid = message.Checkin.Pseudonym.Fingerprint()

	if err := s.peerset.Trust(message.Checkin.Pseudonym); err != nil {
		// The only realistic error is a duplicate checkin, which is a massive
		// protocol violation (participants use ephemeral IDs), so make things
		// fail loudly.
		logger.Error("Failed to check user in", "id", uid, "err", err)
		return err
	}
	// If there was no error, check the participant in internally too and notify
	// the event host to persist the new status.
	logger.Info("Participant checked in", "pseudonym", uid)

	s.lock.Lock()
	s.infos.Participants[uid] = message.Checkin.Pseudonym
	s.infos.Updated = time.Now()
	s.lock.Unlock()

	s.host.OnUpdate(s.infos.Identity.Fingerprint(), s)

	if err := enc.Encode(&Envelope{CheckinAck: &CheckinAck{}}); err != nil {
		logger.Warn("Failed to send checkin ack", "err", err)
		return err
	}
	return nil
}

// handleV1CheckIn is the network handler for the v1 `event` protocol's checkin
// phase.
func (c *Client) handleV1CheckIn(uid tornet.IdentityFingerprint, conn net.Conn, enc *gob.Encoder, dec *gob.Decoder, logger log.Logger) {
	logger.Info("Checking in to event", "pseudonym", c.infos.Pseudonym.Fingerprint())

	// The entire exchange is time limited, ensure failure if it's exceeded
	conn.SetDeadline(time.Now().Add(checkinTimeout))

	// Create the checkin request, digitally signed with the pseudonym
	if err := enc.Encode(&Envelope{Checkin: &Checkin{
		Pseudonym: c.infos.Pseudonym.Public(),
		Signature: c.infos.Pseudonym.Sign(c.infos.Identity),
	}}); err != nil {
		logger.Warn("Failed to send checkin", "err", err)
		c.checkin <- err
		return
	}
	// Read the checkin ack before finalizing the event client
	message := new(Envelope)
	if err := dec.Decode(message); err != nil {
		logger.Warn("Failed to read checkin ack", "err", err)
		c.checkin <- err
		return
	}
	if message.CheckinAck == nil {
		logger.Warn("Received unknown ack message")
		c.checkin <- errors.New("unknown checkin ack")
		return
	}
	// Checkin successful, notify the blocked constructor
	logger.Info("Checked in to event", "pseudonym", c.infos.Pseudonym.Fingerprint())
	c.checkin <- nil
}
