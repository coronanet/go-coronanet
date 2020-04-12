// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package events

import (
	"context"
	"encoding/gob"
	"io"
	"net"
	"sync"
	"time"

	"github.com/coronanet/go-coronanet/params"
	"github.com/coronanet/go-coronanet/protocols"
	"github.com/coronanet/go-coronanet/tornet"
	"github.com/ethereum/go-ethereum/log"
	"golang.org/x/crypto/sha3"
)

// clientDialRequest is a request to reprioritize the current dial schedule to
// the given priority, also enforcing a different initial dial timeout id needed.
type clientDialRequest struct {
	time time.Time
	prio time.Duration
}

// Guest defines the methods needed to join a live event. They revolve around
// persisting updates into the database.
type Guest interface {
	// Status retrieves the guests last known infection status within the given
	// time interval. The method should return every data to make a crypto proof.
	Status(start, end time.Time) (id tornet.SecretIdentity, name string, status string, message string)

	// OnUpdate is invoked when the internal stats of the event changes. All the
	// changes should be persisted to disk to allow recovering. This method does
	// not get passed the updated infos to avoid a data race overwriting something.
	OnUpdate(event tornet.IdentityFingerprint, client *Client)

	// OnBanner is invoked when the banner image of the event changes. Opposed to
	// the OnUpdate, here we actually send the banner along. This might be racey
	// if the protocol allowed frequent banner updates, but since we explicitly
	// forbid it, this is simpler.
	OnBanner(event tornet.IdentityFingerprint, banner []byte)
}

// ClientInfos is all the data maintained about a remote event. It is pre-tagged
// with JSON tags so that calling packages can serialize it to disk without the
// need to reinterpret and maintain all the fields themselves.
type ClientInfos struct {
	Identity  tornet.PublicIdentity `json:"identity"`  // Permanent identity of an event
	Address   tornet.PublicAddress  `json:"address"`   // Permanent address of an event
	Checkin   tornet.SecretIdentity `json:"checkin"`   // Identity to use for checkin
	Pseudonym tornet.SecretIdentity `json:"pseudonym"` // Identity to use for reading stats

	Name   string    `json:"name"`   // Name of the event
	Banner [32]byte  `json:"banner"` // Banner image hash of the event
	Start  time.Time `json:"start"`  // Start time of the event
	End    time.Time `json:"end"`    // Conclusion time of the event

	Status string `json:"status"` // Current status reporting to the event (avoid update cycles)

	Attendees uint `json:"attendees"` // Number of participants in the event
	Negatives uint `json:"negatives"` // Participants who reported negative test results
	Suspected uint `json:"suspected"` // Participants who might have been infected
	Positives uint `json:"positives"` // Participants who reported positive infection

	Updated time.Time `json:"updated"` // Time when the event was last modified
	Synced  time.Time `json:"synced"`  // Time when the event was last synced
}

// Client is a remotely hosted event, running a `tornet` client which periodically
// connects to receive any infection status updates.
type Client struct {
	guest   Guest          // Guest running the client for data persistency
	gateway tornet.Gateway // Gateway to dial the event server through
	infos   *ClientInfos   // Complete event metadata and statistics
	banner  []byte         // Banner image cached for quick serving

	peerset *tornet.PeerSet // Peer set handling remote connectivity

	checkin chan error              // Notification channel when checkin finishes
	update  chan *clientDialRequest // Update channel to change the dial priority
	suspend chan bool               // Channel to suspend or resume auto dialing

	teardown   chan chan struct{} // Termination channel to stop future dials
	terminated chan struct{}      // Termination notification channel to unblock update

	logger log.Logger   // Contextual logger to allow adding optional tags
	lock   sync.RWMutex // Mutex protecting the stats from simultaneous updates
}

// CreateClient creates a brand new event client with the given identity and
// address, generating a new pseudonym for checking in with.
func CreateClient(guest Guest, gateway tornet.Gateway, identity tornet.PublicIdentity, address tornet.PublicAddress, checkin tornet.SecretIdentity, logger log.Logger) (*Client, error) {
	pseudonym, err := tornet.GenerateIdentity()
	if err != nil {
		return nil, err
	}
	return RecreateClient(guest, gateway, &ClientInfos{
		Identity:  identity,
		Address:   address,
		Checkin:   checkin,
		Pseudonym: pseudonym,
	}, logger)
}

// RecreateClient reloads a previously existent event client from a persisted
// configuration dump.
func RecreateClient(guest Guest, gateway tornet.Gateway, infos *ClientInfos, logger log.Logger) (*Client, error) {
	client := &Client{
		guest:      guest,
		gateway:    gateway,
		infos:      infos,
		update:     make(chan *clientDialRequest),
		suspend:    make(chan bool),
		teardown:   make(chan chan struct{}),
		terminated: make(chan struct{}),
		logger:     logger,
	}
	client.peerset = tornet.NewPeerSet(tornet.PeerSetConfig{
		Trusted: []tornet.PublicIdentity{infos.Identity},
		Handler: protocols.MakeHandler(protocols.HandlerConfig{
			Protocol: Protocol,
			Handlers: map[uint]protocols.Handler{
				1: client.handleV1,
			},
		}),
		Timeout: connectionIdleTimeout,
		Logger:  logger,
	})
	// If the client is not yet checked in, do it now before returning the client
	if client.infos.Checkin != nil {
		// Dial the event server
		client.checkin = make(chan error)
		done, err := tornet.DialServer(context.TODO(), tornet.DialConfig{
			Gateway:  gateway,
			Address:  client.infos.Address,
			Server:   client.infos.Identity,
			Identity: client.infos.Checkin,
			PeerSet:  client.peerset,
		})
		if err != nil {
			client.peerset.Close()
			return nil, err
		}
		// Server dialed, but authentication may fail, wait for that or checkin
		select {
		case err = <-done:
		case err = <-client.checkin:
		}
		if err != nil {
			client.peerset.Close()
			return nil, err
		}
		client.infos.Checkin = nil
	}
	// Client surely checked in, start the event update loop
	go client.loop()

	logger.Info("Created event client", "event", client.infos.Identity.Fingerprint(), "name", client.infos.Name)
	return client, nil
}

// Close terminates a running event server.
func (c *Client) Close() error {
	quit := make(chan struct{})
	c.teardown <- quit
	<-quit

	return c.peerset.Close()
}

// Infos retrieves a copy of the event client's internal state for persistence.
// The copy is not safe for modification, only from data races.
func (c *Client) Infos() *ClientInfos {
	c.lock.RLock()
	defer c.lock.RUnlock()

	infos := *c.infos
	return &infos
}

// Report requests the client to schedule an dial due to an infection update. The
// method will change the dial priority to high and request an immediate dial too.
func (c *Client) Report() {
	select {
	case c.update <- &clientDialRequest{time: time.Now(), prio: params.EventInfectionUpdateRetry}:
	case <-c.terminated:
	}
}

// Suspend instructs the client to stop auto-dialing. This is useful when the
// network layer gets disabled, since everything will fail anyway.
func (c *Client) Suspend() {
	select {
	case c.suspend <- true:
	case <-c.terminated:
	}
}

// Resume instructs the client to start auto-dialing. This is useful to trigger
// an immediate redial when networking is enabled.
func (c *Client) Resume() {
	select {
	case c.suspend <- false:
	case <-c.terminated:
	}
}

// loop is the scheduler that periodically connects to the event server to fetch
// any updated statistics and to push relevant infection statuses.
func (c *Client) loop() {
	// If termination is requested, notify anyone listening
	defer close(c.terminated)

	// Initiate a dial straight away, schedule afterward
	var (
		nextTime = time.Now()
		nextDial = time.NewTimer(0)
		nextPrio = params.EventStatsRecheck
	)
	logger := c.logger.New("event", c.infos.Identity.Fingerprint())
	for {
		select {
		case quit := <-c.teardown:
			quit <- struct{}{}
			return

		case suspend := <-c.suspend:
			// If networking is suspended, stop auto-dialing, otherwise redial
			// instantly.
			if !nextDial.Stop() { // Both paths touch the dialer
				select {
				case <-nextDial.C:
				default:
				}
			}
			nextTime = time.Now() // Ensures updates don't resume accidentally

			if suspend {
				logger.Debug("Suspending event dialing")
			} else {
				logger.Debug("Resuming event dialing")
				nextDial.Reset(time.Until(nextTime))
			}

		case sched := <-c.update:
			// A schedule priority change was requested, apply if meaningful
			if nextTime.Before(sched.time) {
				logger.Debug("Keeping earlier schedule", "old", nextTime, "new", sched.time)
			} else {
				logger.Debug("Updated dial schedule", "old", nextTime, "new", sched.time)
				nextTime = sched.time
				if !nextDial.Stop() {
					<-nextDial.C
				}
				nextDial.Reset(time.Until(nextTime))
			}
			if nextPrio < sched.prio {
				logger.Debug("Keeping earlier priority", "old", nextPrio, "new", sched.prio)
			} else {
				logger.Debug("Updated dial priority", "old", nextPrio, "new", sched.prio)
				nextPrio = sched.prio
			}

		case <-nextDial.C:
			logger.Debug("Dialing event server")
			if _, err := tornet.DialServer(context.TODO(), tornet.DialConfig{
				Gateway:  c.gateway,
				Address:  c.infos.Address,
				Server:   c.infos.Identity,
				Identity: c.infos.Pseudonym,
				PeerSet:  c.peerset,
			}); err != nil {
				// If dialing failed, reschedule with the same priority as before
				logger.Error("Dialing event failed", "retry", nextPrio, "err", err)
				nextTime = time.Now().Add(nextPrio)
				nextDial.Reset(nextPrio)
			} else {
				// Dialing succeeded, reschedule with the default priority
				logger.Debug("Dialing event succeeded", "schedule", params.EventStatsRecheck)
				nextPrio = params.EventStatsRecheck
				nextTime = time.Now().Add(nextPrio)
				nextDial.Reset(nextPrio)
			}
		}
	}
}

// handleV1 is the network handler for the v1 `event` protocol. This method only
// demultiplexes the checkin and the data exchange phases.
func (c *Client) handleV1(uid tornet.IdentityFingerprint, conn net.Conn, enc *gob.Encoder, dec *gob.Decoder, logger log.Logger) {
	logger = logger.New("event", c.infos.Identity.Fingerprint())

	c.lock.Lock()
	checkin := c.infos.Checkin != nil
	c.infos.Checkin = nil
	c.lock.Unlock()

	// Depending on the protocol phase, descend into checkin or data exchange
	if checkin {
		c.handleV1CheckIn(uid, conn, enc, dec, logger)
		return
	}
	c.handleV1DataExchange(uid, conn, enc, dec, logger)
}

// handleV1DataExchange is the network handler for the v1 `event` protocol's
// data exchange phase.
func (c *Client) handleV1DataExchange(uid tornet.IdentityFingerprint, conn net.Conn, enc *gob.Encoder, dec *gob.Decoder, logger log.Logger) {
	logger.Info("Running event data exchange")

	// If the event metadata is missing, request it
	c.lock.RLock()
	nometa := c.infos.Name == ""
	c.lock.RUnlock()

	if nometa {
		go enc.Encode(&Envelope{GetMetadata: &GetMetadata{}})
	}
	// Attempt to send over the current status and request new stats
	go c.sendStatusReport(logger, enc)
	go enc.Encode(&Envelope{GetStatus: &GetStatus{}})

	// Start processing messages until torn down
	for {
		// Read the next message off the network
		message := new(Envelope)
		if err := dec.Decode(message); err != nil {
			if err != io.EOF {
				log.Warn("Failed to decode message", "err", err)
			}
			return
		}
		// Depending on what we've got, do something meaningful
		switch {
		case message.Metadata != nil:
			logger.Info("Organizer sent event metadata", "name", message.Metadata.Name)

			// Make sure the event metadata is meaningful
			if message.Metadata.Name == "" {
				logger.Warn("Rejecting event without name")
				return
			}
			if len(message.Metadata.Banner) == 0 {
				logger.Warn("Rejecting event without banner")
				return
			}
			// Set the event metadata, unless it was already transmitted
			c.lock.Lock()
			if c.infos.Name != "" {
				logger.Warn("Rejecting event metadata swap")
				c.lock.Unlock()
				return
			}
			c.banner = message.Metadata.Banner
			c.infos.Name = message.Metadata.Name
			c.infos.Banner = sha3.Sum256(c.banner)
			c.lock.Unlock()

			// Event updated, persist it to disk (banner first, otherwise the above hash will break)
			c.guest.OnBanner(c.infos.Identity.Fingerprint(), c.banner)
			c.guest.OnUpdate(c.infos.Identity.Fingerprint(), c)

		case message.Status != nil:
			logger.Info("Organizer sent event status", "status", message.Status)

			// Update the event statistics, no way to verify these
			c.lock.Lock()
			if c.infos.Start == (time.Time{}) {
				c.infos.Start = message.Status.Start
				c.infos.Updated = time.Now()

				// Event was completed just now, maybe send infection status
				go c.sendStatusReport(logger, enc)
			}
			if c.infos.End == (time.Time{}) {
				c.infos.End = message.Status.End
				c.infos.Updated = time.Now()
			}
			if c.infos.Attendees != message.Status.Attendees {
				c.infos.Attendees = message.Status.Attendees
				c.infos.Updated = time.Now()
			}
			if c.infos.Negatives != message.Status.Negatives {
				c.infos.Negatives = message.Status.Negatives
				c.infos.Updated = time.Now()
			}
			if c.infos.Suspected != message.Status.Suspected {
				c.infos.Suspected = message.Status.Suspected
				c.infos.Updated = time.Now()
			}
			if c.infos.Positives != message.Status.Positives {
				c.infos.Positives = message.Status.Positives
				c.infos.Updated = time.Now()
			}
			c.infos.Synced = time.Now()
			c.lock.Unlock()

			// Event updated, persist it to disk
			c.guest.OnUpdate(c.infos.Identity.Fingerprint(), c)

		case message.ReportAck != nil:
			logger.Info("Organizer sent report ack", "status", message.ReportAck.Status)

			// Update the maintained infection status, if possible
			if !validInfectionStatus(message.ReportAck.Status) {
				logger.Warn("Rejecting invalid status")
				return
			}
			c.lock.Lock()
			if !validInfectionTransition(c.infos.Status, message.ReportAck.Status) {
				logger.Warn("Rejecting malicious status ack", "old", c.infos.Status, "new", message.ReportAck.Status)
				c.lock.Unlock()
				return
			}
			c.infos.Status = message.ReportAck.Status
			c.lock.Unlock()

			// Event updated, persist it to disk
			c.guest.OnUpdate(c.infos.Identity.Fingerprint(), c)

		default:
			logger.Warn("Organizer sent unknown message")
			return
		}
	}
}

// sendStatusReport retrieves the guests latest status update for the event's
// runtime and sends it over to the event server.
func (c *Client) sendStatusReport(logger log.Logger, enc *gob.Encoder) error {
	// If we haven't yet retrieved event infos, try again later
	c.lock.RLock()
	start, end, old := c.infos.Start, c.infos.End, c.infos.Status
	c.lock.RUnlock()

	if start == (time.Time{}) {
		logger.Debug("Withholding status from unbounded event")
		return nil
	}
	// If the event is still running, use that as the end time
	if end == (time.Time{}) {
		end = time.Now() // TODO(karalabe): Maybe enforce a maximum duration
	}
	// Retrieve the current status from the guest and report if transition allowed
	id, name, status, message := c.guest.Status(start, end)
	if validInfectionTransition(old, status) {
		logger.Info("Sending over infection status", "name", name, "status", status)

		blob := c.infos.Identity
		blob = append(blob, name...)
		blob = append(blob, status...)
		blob = append(blob, message...)

		return enc.Encode(&Envelope{Report: &Report{
			Name:      name,
			Status:    status,
			Message:   message,
			Identity:  id.Public(),
			Signature: id.Sign(blob),
		}})
	}
	// Status update was rejected, skip transmitting it
	logger.Debug("Status update noop, skipping", "old", old, "new", status)
	return nil
}
