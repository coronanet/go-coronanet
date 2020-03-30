// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package coronanet

import (
	"context"
	"time"

	"github.com/coronanet/go-coronanet/tornet"
	"github.com/ethereum/go-ethereum/log"
)

// schedulerRequest is a request towards the scheduler to establish contact with
// a batch of peers in a maximum designated amount of time.
type schedulerRequest struct {
	request  time.Duration
	contacts []tornet.IdentityFingerprint
}

// scheduler is responsible for scheduling networking data exchanges based on the
// various priorities that events towards contacts might have.
func (b *Backend) scheduler() {
	// If termination is requested, notify anyone listening
	defer close(b.scheduleTerminated)

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
			log.Debug("Next dialing scheduled", "time", time.Until(earliest))
			nextTime.Reset(time.Until(earliest))
			nextChan = nextTime.C
		}
		// Listen for scheduling requests or keyring updates
		select {
		case <-b.scheduleTeardown:
			return

		case keyring := <-b.scheduleKeyring:
			// New keyring received. Schedule dialing any new contacts immediately,
			// remove anyone gone missing.
			for uid := range keyring.Trusted {
				if _, ok := schedule[uid]; !ok {
					log.Debug("Scheduling dial for new contact", "contact", uid)
					schedule[uid] = time.Now()
				}
			}
			for uid := range schedule {
				if _, ok := keyring.Trusted[uid]; !ok {
					log.Debug("Unscheduling dial for dropped contact", "contact", uid)
					delete(schedule, uid)
				}
			}

		case req := <-b.scheduleUpdate:
			// Application layer requested an update to be pushed out to one or
			// more contacts. Merge the request with the current schedule.
			for _, uid := range req.contacts {
				had, ok := schedule[uid]
				old := time.Until(had)
				switch {
				case !ok:
					log.Error("Reschedule requested for unknown contact", "contact", uid, "schedule", req.request)
				case old > req.request:
					log.Debug("Rescheduling dial or earlier time", "contact", uid, "old", old, "new", req.request)
					schedule[nextDial] = time.Now().Add(req.request)
				default:
					log.Trace("Reschedule to later time ignored", "contact", uid, "old", old, "new", req.request)
				}
			}

		case <-nextChan:
			nextChan = nil

			// A scheduled dial was triggered, request the overlay to connect
			b.lock.RLock()
			overlay := b.overlay
			b.lock.RUnlock()

			if overlay == nil {
				// This can only happen if the overlay was torn down at the exact
				// instance some dial triggered (and before the keyring was nuked).
				log.Warn("Scheduler triggered without overlay")
				continue
			}
			log.Debug("Scheduling dial for contact", "contact", nextDial)
			if err := overlay.Dial(context.TODO(), nextDial); err != nil {
				log.Error("Dial request failed", "contact", nextDial, "schedule", schedulerFailureRedial, "err", err)
				schedule[nextDial] = time.Now().Add(schedulerFailureRedial)
			} else {
				// Dialing succeeded, unless someone has anything important, check back tomorrow
				log.Debug("Dialing succeeded, rescheduling", "contact", nextDial, "schedule", schedulerSanityRedial)
				schedule[nextDial] = time.Now().Add(schedulerSanityRedial)
			}
		}
	}
}
