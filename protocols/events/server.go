// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package events

import (
	"crypto/ed25519"
	"encoding/gob"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/coronanet/go-coronanet/params"
	"github.com/coronanet/go-coronanet/protocols"
	"github.com/coronanet/go-coronanet/tornet"
	"github.com/ethereum/go-ethereum/log"
)

// Host defines the methods needed to run a live event. They revolve around
// persisting updates into the database.
type Host interface {
	// OnUpdate is invoked when the internal stats of the event changes. All the
	// changes should be persisted to disk to allow recovering. This method does
	// not get passed the updated infos to avoid a data race overwriting something.
	OnUpdate(event tornet.IdentityFingerprint)

	// OnReport is invoked when an event participant sends in an infection report
	// that changes the status of the event. The organizer may store the message
	// for later verification.
	OnReport(event tornet.IdentityFingerprint, pseudonym tornet.IdentityFingerprint, message string) error
}

// ServerInfos is all the data maintained about a local event. It is pre-tagged
// with JSON tags so that calling packages can serialize it to disk without the
// need to reinterpret and maintain all the fields themselves.
type ServerInfos struct {
	Identity tornet.SecretIdentity `json:"identity"` // Permanent identity of an event
	Address  tornet.SecretAddress  `json:"address"`  // Permanent address of an event

	Participants map[tornet.IdentityFingerprint]tornet.PublicIdentity `json:"participants"` // Anonymous participant credentials
	Identities   map[tornet.IdentityFingerprint]tornet.PublicIdentity `json:"identities"`   // Real participant credentials
	Statuses     map[tornet.IdentityFingerprint]string                `json:"statuses"`     // Participant infection statuses
	Names        map[tornet.IdentityFingerprint]string                `json:"names"`        // Real participant names

	Name   string    `json:"name"`   // Name of the event
	Banner []byte    `json:"banner"` // Banner image of the event
	Start  time.Time `json:"start"`  // Start time of the event
	End    time.Time `json:"end"`    // Conclusion time of the event
}

// Server is a locally hosted event, running a `tornet` server to which any number
// of participants may check in.
type Server struct {
	host  Host         // Organizer running the server for data persistency
	infos *ServerInfos // Complete event metadata and statistics

	checkin tornet.SecretIdentity // Ephemeral identity to check in with
	peerset *tornet.PeerSet       // Peer set handling remote connections
	server  *tornet.Server        // Ephemeral pairing server through the Tor network

	lock sync.RWMutex // Mutex protecting the stats from simultaneous updates
}

// CreateServer creates a brand new event server with the given matadata and a
// new random identity and address.
func CreateServer(host Host, gateway tornet.Gateway, name string, banner []byte) (*Server, error) {
	// Generate the permanent identities of the event
	identity, err := tornet.GenerateIdentity()
	if err != nil {
		return nil, err
	}
	address, err := tornet.GenerateAddress()
	if err != nil {
		return nil, err
	}
	// Assemble the event, ready to be published
	return RecreateServer(host, gateway, &ServerInfos{
		Identity:     identity,
		Address:      address,
		Participants: make(map[tornet.IdentityFingerprint]tornet.PublicIdentity),
		Identities:   make(map[tornet.IdentityFingerprint]tornet.PublicIdentity),
		Statuses:     make(map[tornet.IdentityFingerprint]string),
		Names:        make(map[tornet.IdentityFingerprint]string),
		Name:         name,
		Banner:       banner,
		Start:        time.Now(),
	})
}

// RecreateServer reloads a previously existent event server from a persisted
// configuration dump.
func RecreateServer(host Host, gateway tornet.Gateway, infos *ServerInfos) (*Server, error) {
	// Generate the rotating temporary checkin identity
	checkin, err := tornet.GenerateIdentity()
	if err != nil {
		return nil, err
	}
	// Assemble the server, ready to be published
	trusted := make([]tornet.PublicIdentity, 0, len(infos.Participants)+1)
	for _, id := range infos.Participants {
		trusted = append(trusted, id)
	}
	trusted = append(trusted, checkin.Public())

	server := &Server{
		host:    host,
		infos:   infos,
		checkin: checkin,
	}
	// Start the server to accept inbound connections
	server.peerset = tornet.NewPeerSet(tornet.PeerSetConfig{
		Trusted: trusted,
		Handler: protocols.MakeHandler(protocols.HandlerConfig{
			Protocol: Protocol,
			Handlers: map[uint]protocols.Handler{
				1: server.handleV1,
			},
		}),
		Timeout: connectionIdleTimeout,
	})
	server.server, err = tornet.NewServer(tornet.ServerConfig{
		Gateway:  gateway,
		Address:  server.infos.Address,
		Identity: server.infos.Identity,
		PeerSet:  server.peerset,
	})
	if err != nil {
		server.peerset.Close()
		return nil, err
	}
	log.Info("Created event server", "event", server.infos.Identity.Fingerprint(), "name", server.infos.Name)
	return server, nil
}

// Close terminates a running event server.
func (s *Server) Close() error {
	return s.peerset.Close()
}

// Infos retrieves a copy of the event server's internal state for persistence.
// The copy is not safe for modification, only from data races.
func (s *Server) Infos() *ServerInfos {
	s.lock.RLock()
	defer s.lock.RUnlock()

	infos := *s.infos

	infos.Participants = make(map[tornet.IdentityFingerprint]tornet.PublicIdentity)
	for uid, id := range s.infos.Participants {
		infos.Participants[uid] = id
	}
	infos.Identities = make(map[tornet.IdentityFingerprint]tornet.PublicIdentity)
	for uid, id := range s.infos.Identities {
		infos.Identities[uid] = id
	}
	infos.Statuses = make(map[tornet.IdentityFingerprint]string)
	for uid, status := range s.infos.Statuses {
		infos.Statuses[uid] = status
	}
	return &infos
}

// handleV1 is the network handler for the v1 `event` protocol. This method only
// demultiplexes the checkin and the data exchange phases.
func (s *Server) handleV1(logger log.Logger, uid tornet.IdentityFingerprint, conn net.Conn, enc *gob.Encoder, dec *gob.Decoder) {
	// Add the event id to the logger in case of concurrent events
	logger = logger.New("event", s.infos.Identity.Fingerprint())

	// If the connection is a checkin, rotate the key. Note, the `tornet` peer set
	// already deduplicates connections from the same identity, so it's impossible
	// for two check-ins to clash.
	s.lock.Lock()
	checkin := s.checkin.Fingerprint() == uid
	if checkin {
		// First connection crossed the checkin barrier, rotate the key out
		if newauth, err := tornet.GenerateIdentity(); err != nil {
			logger.Error("Failed to generate checkin credentials", "err", err)
			s.checkin = tornet.SecretIdentity{} // Don't leave as nil
		} else {
			s.checkin = newauth
		}
		// Doesn't matter how the connection ends, ephemerally authenticated peer
		// is not allowed back in. A successful checkin will permit them to enter
		// using permanent authentication credentials.
		defer func() {
			if err := s.peerset.Untrust(uid); err != nil {
				logger.Error("Failed to untrust ephemeral credential", "err", err)
			}
		}()
	}
	s.lock.Unlock()

	// Depending on the protocol phase, descend into checkin or data exchange
	if checkin {
		s.handleV1CheckIn(logger, uid, conn, enc, dec)
		return
	}
	s.handleV1DataExchange(logger, uid, conn, enc, dec)
}

// handleV1CheckIn is the network handler for the v1 `event` protocol's checkin
// phase.
func (s *Server) handleV1CheckIn(logger log.Logger, uid tornet.IdentityFingerprint, conn net.Conn, enc *gob.Encoder, dec *gob.Decoder) {
	logger.Info("Participant checking in")

	// The entire exchange is time limited, ensure failure if it's exceeded
	conn.SetDeadline(time.Now().Add(checkinTimeout))

	// Read the checkin request and validate the digital signature
	message := new(Envelope)
	if err := dec.Decode(message); err != nil {
		logger.Warn("Checkin retrieval failed", "err", err)
		return
	}
	if message.Checkin == nil {
		logger.Warn("Checkin message missing")
		return
	}
	if len(message.Checkin.Pseudonym) != ed25519.PublicKeySize {
		logger.Warn("Invalid checkin identity length", "bytes", len(message.Checkin.Pseudonym))
		return
	}
	if len(message.Checkin.Signature) != ed25519.SignatureSize {
		logger.Warn("Invalid checkin signature length", "bytes", len(message.Checkin.Signature))
		return
	}
	if !message.Checkin.Pseudonym.Verify(s.infos.Identity.Public(), message.Checkin.Signature) {
		logger.Warn("Invalid checkin signature")
		return
	}
	// Checkin completed, authorize the identity to connect for data exchange
	uid = message.Checkin.Pseudonym.Fingerprint()

	if err := s.peerset.Trust(message.Checkin.Pseudonym); err != nil {
		// The only realistic error is a duplicate checkin, which is a massive
		// protocol violation (participants use ephemeral IDs), so make things
		// fail loudly.
		logger.Error("Failed to check user in", "id", uid, "err", err)
		return
	}
	// If there was no error, check the participant in internally too and notify
	// the event host to persist the new status.
	logger.Info("Participant checked in", "pseudonym", uid)

	s.lock.Lock()
	s.infos.Participants[uid] = message.Checkin.Pseudonym
	s.lock.Unlock()

	s.host.OnUpdate(s.infos.Identity.Fingerprint())

	if err := enc.Encode(&Envelope{CheckinAck: &CheckinAck{}}); err != nil {
		logger.Warn("Failed to send checkin ack", "err", err)
		return
	}
}

// handleV1DataExchange is the network handler for the v1 `event` protocol's
// data exchange phase.
func (s *Server) handleV1DataExchange(logger log.Logger, uid tornet.IdentityFingerprint, conn net.Conn, enc *gob.Encoder, dec *gob.Decoder) {
	logger.Info("Running event data exchange")

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
		case message.GetMetadata != nil:
			logger.Info("Participant requested event metadata")
			if err := enc.Encode(&Envelope{Metadata: &Metadata{
				Name:   s.infos.Name,
				Banner: s.infos.Banner,
			}}); err != nil {
				logger.Warn("Failed to send event metadata", "err", err)
				return
			}

		case message.GetStatus != nil:
			logger.Info("Participant requested event status")

			// Gather all the shareable event statistics
			s.lock.RLock()
			reply := &Status{
				Start:     s.infos.Start,
				End:       s.infos.End,
				Attendees: uint(len(s.infos.Participants)),
			}
			for _, status := range s.infos.Statuses {
				switch status {
				case params.InfectionStatusNegative:
					reply.Negatives++
				case params.InfectionStatusSuspected:
					reply.Suspected++
				case params.InfectionStatusPositive:
					reply.Positives++
				case params.InfectionStatusUnknown:
				// Do nothing
				default:
					panic(fmt.Sprintf("unknown infection status: %s", status))
				}
			}
			// Merge the organizer into the attendees too
			reply.Attendees++

			s.lock.RUnlock()

			// Package up and send over the statistics
			if err := enc.Encode(&Envelope{Status: reply}); err != nil {
				logger.Warn("Failed to send event status", "err", err)
				return
			}

		case message.Report != nil:
			logger.Info("Participant sent infection report")

			// Sanity check the report identity fields
			if len(message.Report.Identity) != ed25519.PublicKeySize {
				logger.Warn("Invalid report identity length", "bytes", len(message.Report.Identity))
				return
			}
			if len(message.Report.Signature) != ed25519.SignatureSize {
				logger.Warn("Invalid report signature length", "bytes", len(message.Report.Signature))
				return
			}
			// Validate all the data and drop the connection if it fails
			blob := s.infos.Identity.Public()
			blob = append(blob, message.Report.Name...)
			blob = append(blob, message.Report.Status...)
			blob = append(blob, message.Report.Message...)

			if !message.Report.Identity.Verify(blob, message.Report.Signature) {
				logger.Warn("Invalid report signature")
				return
			}
			if len(message.Report.Name) == 0 {
				logger.Warn("Report contains empty name")
				return
			}
			if !validInfectionStatus(message.Report.Status) {
				logger.Warn("Report contains invalid status", "status", message.Report.Status)
				return
			}
			// If content seems valid, integrate the report into the event stats
			s.lock.Lock()
			cid := message.Report.Identity
			if old, ok := s.infos.Identities[uid]; ok && old.Fingerprint() != cid.Fingerprint() {
				// Changing a user identity is a serious protocol violation and
				// cannot happen by accident. Make sure the failure is loud.
				logger.Error("Identity swap attempted", "old", old.Fingerprint(), "current", cid.Fingerprint())
				s.lock.Unlock()
				return
			}
			s.infos.Identities[uid] = cid

			status := message.Report.Status
			if old, ok := s.infos.Statuses[uid]; ok && !validInfectionTransition(old, status) {
				logger.Warn("Ignoring invalid status update", "status", status)
				s.lock.Unlock()

				if err := enc.Encode(&Envelope{ReportAck: &ReportAck{Status: old}}); err != nil {
					logger.Warn("Failed to send report ack", "err", err)
					return
				}
				continue
			}
			s.infos.Statuses[uid] = status

			if _, ok := s.infos.Names[uid]; !ok {
				// Users can for valid reasons change names, but let's not care about them
				s.infos.Names[uid] = message.Report.Name
			}
			s.lock.Unlock()

			// Status update accepted, ensure it's persisted to disk
			s.host.OnUpdate(s.infos.Identity.Fingerprint())
			s.host.OnReport(s.infos.Identity.Fingerprint(), uid, message.Report.Message)

			if err := enc.Encode(&Envelope{ReportAck: &ReportAck{Status: status}}); err != nil {
				logger.Warn("Failed to send report ack", "err", err)
				return
			}

		default:
			logger.Warn("Participant sent unknown message")
			return
		}
	}
}
