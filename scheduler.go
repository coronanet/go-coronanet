// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package coronanet

import (
	"context"
	"time"

	"github.com/coronanet/go-coronanet/protocols/corona"
	"github.com/coronanet/go-coronanet/tornet"
)

// schedulerRequest is a request towards the scheduler to establish contact with
// a batch of peers in a maximum designated amount of time.
type schedulerRequest struct {
	request  time.Duration
	contacts []tornet.IdentityFingerprint
}

// scheduler is a remote connection dialer that aggregates various system and
// user events and schedules the dialing of remote peers based on them.
type scheduler struct {
	backend *Backend // Backend to retrieve the overlay node from

	update     chan *schedulerRequest    // Scheduler channel for app update requests
	keyring    chan tornet.SecretKeyRing // Scheduler channel when the keyring is updated
	teardown   chan chan struct{}        // Scheduler channel when the system is terminating
	terminated chan struct{}             // Termination channel to unblock any schedules
}

// newScheduler creates a new dial scheduler.
func newScheduler(backend *Backend) *scheduler {
	dialer := &scheduler{
		backend:    backend,
		update:     make(chan *schedulerRequest),
		keyring:    make(chan tornet.SecretKeyRing),
		teardown:   make(chan chan struct{}),
		terminated: make(chan struct{}),
	}
	go dialer.loop()
	return dialer
}

// close terminates the dial scheduler.
func (s *scheduler) close() error {
	closer := make(chan struct{})
	s.teardown <- closer
	<-closer

	return nil
}

// suspend sends and empty keyring to the scheduler, causing it to remove all
// pending dial tasks.
func (s *scheduler) suspend() {
	select {
	case s.keyring <- tornet.SecretKeyRing{}:
	case <-s.terminated:
	}
}

// reinit sends a secret keyring to the scheduler, causing it to recheck all its
// internals, dropping anyone not in the ney keyring and scheduling new contacts.
func (s *scheduler) reinit(keyring tornet.SecretKeyRing) {
	select {
	case s.keyring <- keyring:
	case <-s.terminated:
	}
}

// prioritize updates all the specified contacts to be dial within the requested
// time allowance at latest. They may be dialed sooner.
func (s *scheduler) prioritize(dial time.Duration, contacts []tornet.IdentityFingerprint) {
	select {
	case s.update <- &schedulerRequest{request: dial, contacts: contacts}:
	case <-s.terminated:
	}
}

// loop is responsible for scheduling networking data exchanges based on the various
// priorities that events towards contacts might have.
func (s *scheduler) loop() {
	// If termination is requested, notify anyone listening
	defer close(s.terminated)

	schedule := make(map[tornet.IdentityFingerprint]time.Time)

	var (
		nextTime = time.NewTimer(0)
		nextChan = nextTime.C
		nextDial tornet.IdentityFingerprint
	)
	for {
		// Something happened, find the next dial target
		if nextChan != nil {
			if !nextTime.Stop() {
				<-nextTime.C
			}
			nextChan = nil
		}
		var earliest time.Time
		for uid, time := range schedule {
			if earliest.IsZero() || earliest.After(time) {
				earliest, nextDial = time, uid
			}
		}
		if !earliest.IsZero() {
			s.backend.logger.Debug("Next dialing scheduled", "time", time.Until(earliest))
			nextTime.Reset(time.Until(earliest))
			nextChan = nextTime.C
		}
		// Listen for scheduling requests or keyring updates
		select {
		case quit := <-s.teardown:
			quit <- struct{}{}
			return

		case keyring := <-s.keyring:
			// New keyring received. Schedule dialing any new contacts immediately,
			// remove anyone gone missing.
			for uid := range keyring.Trusted {
				if _, ok := schedule[uid]; !ok {
					s.backend.logger.Debug("Scheduling dial for new contact", "contact", uid)
					schedule[uid] = time.Now()
				}
			}
			for uid := range schedule {
				if _, ok := keyring.Trusted[uid]; !ok {
					s.backend.logger.Debug("Unscheduling dial for dropped contact", "contact", uid)
					delete(schedule, uid)
				}
			}

		case req := <-s.update:
			// Application layer requested an update to be pushed out to one or
			// more contacts. Merge the request with the current schedule.
			for _, uid := range req.contacts {
				had, ok := schedule[uid]
				old := time.Until(had)
				switch {
				case !ok:
					s.backend.logger.Error("Reschedule requested for unknown contact", "contact", uid, "schedule", req.request)
				case old > req.request:
					s.backend.logger.Debug("Rescheduling dial or earlier time", "contact", uid, "old", old, "new", req.request)
					schedule[nextDial] = time.Now().Add(req.request)
				default:
					s.backend.logger.Trace("Reschedule to later time ignored", "contact", uid, "old", old, "new", req.request)
				}
			}

		case <-nextChan:
			nextChan = nil

			// A scheduled dial was triggered, request the overlay to connect
			s.backend.lock.RLock()
			overlay := s.backend.overlay
			s.backend.lock.RUnlock()

			if overlay == nil {
				// This can only happen if the overlay was torn down at the exact
				// instance some dial triggered (and before the keyring was nuked).
				s.backend.logger.Warn("Scheduler triggered without overlay")
				continue
			}
			s.backend.logger.Debug("Scheduling dial for contact", "contact", nextDial)
			if _, err := overlay.Dial(context.TODO(), nextDial); err != nil {
				s.backend.logger.Error("Dial request failed", "contact", nextDial, "schedule", schedulerFailureRedial, "err", err)
				schedule[nextDial] = time.Now().Add(schedulerFailureRedial)
			} else {
				// Dialing succeeded, unless someone has anything important, check back tomorrow
				s.backend.logger.Debug("Dialing succeeded, rescheduling", "contact", nextDial, "schedule", schedulerSanityRedial)
				schedule[nextDial] = time.Now().Add(schedulerSanityRedial)
			}
		}
	}
}

// broadcast tries to broadcast a message to all active peers, and for everyone
// else it schedules a prioritized dial.
func (b *Backend) broadcast(message *corona.Envelope, priority time.Duration) {
	// Retrieve the list of contacts to broadcast to
	prof, err := b.Profile()
	if err != nil {
		b.logger.Error("Broadcasting without profile", "err", err)
		return
	}
	// Send to everyone online, gather anyone offline
	var offline []tornet.IdentityFingerprint

	for uid := range prof.KeyRing.Trusted {
		if enc := b.peerset[uid]; enc != nil {
			go enc.Encode(message)
		} else {
			offline = append(offline, uid)
		}
	}
	// If anyone was offline, schedule it to them later
	if len(offline) > 0 {
		b.dialer.prioritize(priority, offline)
	}
}
