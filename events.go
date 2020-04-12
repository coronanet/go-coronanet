// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package coronanet

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/coronanet/go-coronanet/params"
	"github.com/coronanet/go-coronanet/protocols/events"
	"github.com/coronanet/go-coronanet/tornet"
	"github.com/syndtr/goleveldb/leveldb/util"
)

var (
	// dbHostedEventPrefix is the database key for storing a hosted event's infos.
	dbHostedEventPrefix = []byte("hosted-")

	// dbJoinedEventPrefix is the database key for storing a joined event's infos.
	dbJoinedEventPrefix = []byte("joined-")

	// ErrEventNotFound is returned if a n event is attempted to be accessed but
	// it is not found.
	ErrEventNotFound = errors.New("event not found")

	// ErrCheckinNotInProgress is returned if a caller attempts to wait for a
	// checkin session on an event, but none is in progress.
	ErrCheckinNotInProgress = errors.New("checkin not in progress")

	// ErrEventAlreadyJoined is returned if an event is attempted to be joined
	// that the local user is already a member of.
	ErrEventAlreadyJoined = errors.New("event already joined")
)

// eventHost is an alias for the backend which implements the events.Host interface.
type eventHost Backend

// Banner is invoked when a participants asks for the event metadata (which
// includes the banner picture), but it is not yet cached in the server.
//
// If something unexpected happens, this method returns nil to force the event
// server to retry for the next invocation.
func (h *eventHost) Banner(event tornet.IdentityFingerprint, server *events.Server) []byte {
	infos := server.Infos()
	if infos.Banner == ([32]byte{}) {
		return []byte{}
	}
	blob, err := (*Backend)(h).CDNImage(infos.Banner)
	if err != nil {
		return nil
	}
	return blob
}

// OnUpdate is invoked when the internal stats of the event changes. All the
// changes should be persisted to disk to allow recovering. This method does
// not get passed the updated infos to avoid a data race overwriting something.
func (h *eventHost) OnUpdate(event tornet.IdentityFingerprint, server *events.Server) {
	blob, err := json.Marshal(server.Infos())
	if err != nil {
		h.logger.Error("Failed to marshal event infos", "event", event, "err", err)
		return
	}
	if err := h.database.Put(append(dbHostedEventPrefix, event...), blob, nil); err != nil {
		h.logger.Error("Failed to store event infos", "event", event, "err", err)
		return
	}
}

// OnReport is invoked when an event participant sends in an infection report
// that changes the status of the event. The organizer may store the message
// for later verification.
func (h *eventHost) OnReport(event tornet.IdentityFingerprint, server *events.Server, pseudonym tornet.IdentityFingerprint, message string) error {
	h.logger.Error("Event report handler not implemented", "event", event, "pseudonym", pseudonym, "message", message)
	return nil
}

// eventGuest is an alias for the backend which implements the events.Guest interface.
type eventGuest Backend

// Status retrieves the guests last known infection status within the given
// time interval. The method should return every data to make a crypto proof.
func (g *eventGuest) Status(start, end time.Time) (id tornet.SecretIdentity, name string, status string, message string) {
	return nil, "", "", ""
}

// OnUpdate is invoked when the internal stats of the event changes. All the
// changes should be persisted to disk to allow recovering. This method does
// not get passed the updated infos to avoid a data race overwriting something.
func (g *eventGuest) OnUpdate(event tornet.IdentityFingerprint, client *events.Client) {
	blob, err := json.Marshal(client.Infos())
	if err != nil {
		g.logger.Error("Failed to marshal event infos", "event", event, "err", err)
		return
	}
	if err := g.database.Put(append(dbJoinedEventPrefix, event...), blob, nil); err != nil {
		g.logger.Error("Failed to store event infos", "event", event, "err", err)
		return
	}
}

// OnBanner is invoked when the banner image of the event changes. Opposed to
// the OnUpdate, here we actually send the banner along. This might be racey
// if the protocol allowed frequent banner updates, but since we explicitly
// forbid it, this is simpler.
func (g *eventGuest) OnBanner(event tornet.IdentityFingerprint, banner []byte) {
	if err := (*Backend)(g).uploadJoinedEventBanner(event, banner); err != nil {
		g.logger.Error("Failed to store event banner", "event", event, "err", err)
		return
	}
}

// initEvents iterates over all the hosted and joined events in the database
// and recreates a new server or client for every one that is still running or
// is in its maintenance period.
func (b *Backend) initEvents() error {
	// Sanity check that we're not doing crazy things
	b.logger.Info("Recreating tracked events")
	if b.hosted != nil {
		panic("inited events outside of startup")
	}
	// Recreate all the known hosted events, but tear down if any fails
	reinitHosted := func(event tornet.IdentityFingerprint) (*events.Server, error) {
		infos, err := b.HostedEvent(event)
		if err != nil {
			return nil, err
		}
		if infos.End != (time.Time{}) && time.Since(infos.End) > params.EventMaintenancePeriod {
			b.logger.Info("Event exceeded maintenance period", "event", event, "ended", time.Since(infos.End))
			return nil, nil
		}
		return events.RecreateServer((*eventHost)(b), tornet.NewTorGateway(b.network), infos)
	}
	hosted := make(map[tornet.IdentityFingerprint]*events.Server)
	for _, event := range b.HostedEvents() {
		server, err := reinitHosted(event)
		if err != nil {
			for _, created := range hosted {
				created.Close()
			}
			return err
		}
		if server != nil {
			hosted[event] = server
		}
	}
	// Recreate all the known joined events, but tear down if any fails
	reinitJoined := func(event tornet.IdentityFingerprint) (*events.Client, error) {
		infos, err := b.JoinedEvent(event)
		if err != nil {
			return nil, err
		}
		if infos.End != (time.Time{}) && time.Since(infos.End) > params.EventMaintenancePeriod {
			b.logger.Info("Event exceeded maintenance period", "event", event, "ended", time.Since(infos.End))
			return nil, nil
		}
		return events.RecreateClient((*eventGuest)(b), tornet.NewTorGateway(b.network), infos)
	}
	joined := make(map[tornet.IdentityFingerprint]*events.Client)
	for _, event := range b.JoinedEvents() {
		client, err := reinitJoined(event)
		if err != nil {
			for _, created := range joined {
				created.Close()
			}
			for _, created := range hosted {
				created.Close()
			}
			return err
		}
		if client != nil {
			joined[event] = client
		}
	}
	b.hosted = hosted
	b.checkin = make(map[tornet.IdentityFingerprint]*events.CheckinSession)
	b.joined = joined

	return nil
}

// nukeEvents tears down all the hosted and joined events. This method should be
// only use on shutdown or when deleting a profile.
func (b *Backend) nukeEvents() error {
	b.logger.Info("Stopping tracked events")
	for _, event := range b.hosted {
		event.Close()
	}
	b.hosted = nil

	for _, event := range b.joined {
		event.Close()
	}
	b.joined = nil

	return nil
}

// CreateEvent assembles a new Corona Network event server.
func (b *Backend) CreateEvent(name string) (tornet.IdentityFingerprint, error) {
	b.logger.Info("Creating new event", "name", name)

	// THe local user is a participant of all events, make sure it exists
	if _, err := b.Profile(); err != nil {
		return "", err
	}
	server, err := events.CreateServer((*eventHost)(b), tornet.NewTorGateway(b.network), name, [32]byte{})
	if err != nil {
		return "", err
	}
	// Server successfully started, insert it into the database
	infos := server.Infos()
	event := infos.Identity.Fingerprint()

	blob, err := json.Marshal(infos)
	if err != nil {
		return "", err
	}
	if err := b.database.Put(append(dbHostedEventPrefix, event...), blob, nil); err != nil {
		server.Close()
		return "", err
	}
	// Event hosted and persisted to disk, add it to the tracked servers
	b.lock.Lock()
	defer b.lock.Unlock()

	b.hosted[event] = server
	return event, nil
}

// TerminateEvent marks the end time of the event, disallowing further participants
// from checking in. It also triggers the maintenance period, after which the server
// is torn down.
func (b *Backend) TerminateEvent(event tornet.IdentityFingerprint) error {
	b.logger.Info("Terminating event", "event", event)

	// Retrieve the server and mark the event completed
	b.lock.Lock()
	defer b.lock.Unlock()

	server, ok := b.hosted[event]
	if !ok {
		return ErrEventNotFound
	}
	if err := server.Terminate(); err != nil {
		return err
	}
	// Push the termination updates into the database too
	blob, err := json.Marshal(server.Infos())
	if err != nil {
		return err
	}
	return b.database.Put(append(dbHostedEventPrefix, event...), blob, nil)
}

// HostedEvents returns the unique ids of all the hosted events.
func (b *Backend) HostedEvents() []tornet.IdentityFingerprint {
	events := []tornet.IdentityFingerprint{} // Need explicit init for JSON!

	it := b.database.NewIterator(util.BytesPrefix(dbHostedEventPrefix), nil)
	defer it.Release()

	for it.Next() {
		events = append(events, tornet.IdentityFingerprint(it.Key()[len(dbHostedEventPrefix):]))
	}
	return events
}

// HostedEvent retrieves all the known information about a hosted event.
func (b *Backend) HostedEvent(event tornet.IdentityFingerprint) (*events.ServerInfos, error) {
	blob, err := b.database.Get(append(dbHostedEventPrefix, event...), nil)
	if err != nil {
		return nil, ErrEventNotFound
	}
	infos := new(events.ServerInfos)
	if err := json.Unmarshal(blob, infos); err != nil {
		return nil, err
	}
	return infos, nil
}

// UploadHostedEventBanner uploads a new banner picture for the hosted event.
func (b *Backend) UploadHostedEventBanner(event tornet.IdentityFingerprint, data []byte) error {
	b.logger.Info("Uploading hosted event banner", "event", event)

	b.lock.Lock()
	defer b.lock.Unlock()

	// Retrieve the current event to ensure it exists and still running
	infos, err := b.HostedEvent(event)
	if err != nil {
		return err
	}
	if infos.End != (time.Time{}) {
		return events.ErrEventConcluded
	}
	// Upload the image into the CDN and delete the old one
	hash, err := b.uploadCDNImage(data)
	if err != nil {
		return err
	}
	if infos.Banner != ([32]byte{}) {
		if err := b.deleteCDNImage(infos.Banner); err != nil {
			return err
		}
	}
	// If the hash changed, update the event
	if infos.Banner == hash {
		return nil
	}
	infos.Banner = hash

	blob, err := json.Marshal(infos)
	if err != nil {
		return err
	}
	if err := b.database.Put(append(dbHostedEventPrefix, event...), blob, nil); err != nil {
		return err
	}
	// Banner swapped out, ping the server too
	if server, ok := b.hosted[event]; ok {
		server.Update(infos.Banner)
	}
	return nil
}

// DeleteHostedEventBanner deletes the existing banner picture of the hosted event.
func (b *Backend) DeleteHostedEventBanner(event tornet.IdentityFingerprint) error {
	b.logger.Info("Deleting hosted event banner", "event", event)

	b.lock.Lock()
	defer b.lock.Unlock()

	// Retrieve the current event to ensure it exists
	infos, err := b.HostedEvent(event)
	if err != nil {
		return err
	}
	if infos.Banner == [32]byte{} {
		return nil
	}
	// Profile picture exists, delete it from the CDN and update the profile
	if err := b.deleteCDNImage(infos.Banner); err != nil {
		return err
	}
	infos.Banner = [32]byte{}

	blob, err := json.Marshal(infos)
	if err != nil {
		return err
	}
	return b.database.Put(append(dbHostedEventPrefix, event...), blob, nil)
}

// InitEventCheckin retrieves the current access and checkin credentials of a
// hosted event. If none exists, it creates a new one.
func (b *Backend) InitEventCheckin(event tornet.IdentityFingerprint) (*events.CheckinSession, error) {
	b.logger.Info("Creating checkin session", "event", event)

	b.lock.Lock()
	defer b.lock.Unlock()

	if b.overlay == nil {
		return nil, ErrNetworkDisabled
	}
	server, ok := b.hosted[event]
	if !ok {
		return nil, ErrEventNotFound
	}
	if session, ok := b.checkin[event]; ok {
		return session, nil
	}
	session, err := server.Checkin()
	if err != nil {
		return nil, err
	}
	b.checkin[event] = session
	return session, nil
}

// WaitEventCheckin waits for a checkin session to conclude.
func (b *Backend) WaitEventCheckin(event tornet.IdentityFingerprint) error {
	b.logger.Info("Waiting for checkin session", "event", event)

	// Ensure there is a checkin ongoing
	b.lock.RLock()
	session := b.checkin[event]
	b.lock.RUnlock()

	if session == nil {
		return ErrCheckinNotInProgress
	}
	// Session live, wait for it
	return session.Wait(context.TODO())
}

// JoinEventCheckin joins a remotely initiated event checkin process.
func (b *Backend) JoinEventCheckin(id tornet.PublicIdentity, address tornet.PublicAddress, auth tornet.SecretIdentity) error {
	b.logger.Info("Joining for checkin session", "event", id.Fingerprint())

	if b.overlay == nil {
		return ErrNetworkDisabled
	}
	if _, err := b.JoinedEvent(id.Fingerprint()); err == nil {
		return ErrEventAlreadyJoined
	}
	client, err := events.CreateClient((*eventGuest)(b), tornet.NewTorGateway(b.network), id, address, auth)
	if err != nil {
		return err
	}
	// Server successfully joined, insert it into the database
	infos := client.Infos()
	event := infos.Identity.Fingerprint()

	blob, err := json.Marshal(infos)
	if err != nil {
		return err
	}
	if err := b.database.Put(append(dbJoinedEventPrefix, event...), blob, nil); err != nil {
		client.Close()
		return err
	}
	// Event hosted and persisted to disk, add it to the tracked servers
	b.lock.Lock()
	defer b.lock.Unlock()

	b.joined[event] = client
	return nil
}

// JoinedEvents returns the unique ids of all the joined events.
func (b *Backend) JoinedEvents() []tornet.IdentityFingerprint {
	events := []tornet.IdentityFingerprint{} // Need explicit init for JSON!

	it := b.database.NewIterator(util.BytesPrefix(dbJoinedEventPrefix), nil)
	defer it.Release()

	for it.Next() {
		events = append(events, tornet.IdentityFingerprint(it.Key()[len(dbJoinedEventPrefix):]))
	}
	return events
}

// JoinedEvent retrieves all the known information about a joined event.
func (b *Backend) JoinedEvent(event tornet.IdentityFingerprint) (*events.ClientInfos, error) {
	blob, err := b.database.Get(append(dbJoinedEventPrefix, event...), nil)
	if err != nil {
		return nil, ErrEventNotFound
	}
	infos := new(events.ClientInfos)
	if err := json.Unmarshal(blob, infos); err != nil {
		return nil, err
	}
	return infos, nil
}

// uploadJoinedEventBanner uploads a new banner picture for the joined event.
func (b *Backend) uploadJoinedEventBanner(event tornet.IdentityFingerprint, data []byte) error {
	b.logger.Info("Uploading joined event banner", "event", event)

	b.lock.Lock()
	defer b.lock.Unlock()

	// Retrieve the current event to ensure it exists and still running
	infos, err := b.JoinedEvent(event)
	if err != nil {
		return err
	}
	if infos.End != (time.Time{}) {
		return events.ErrEventConcluded
	}
	// Upload the image into the CDN and delete the old one
	hash, err := b.uploadCDNImage(data)
	if err != nil {
		return err
	}
	if infos.Banner != ([32]byte{}) {
		if err := b.deleteCDNImage(infos.Banner); err != nil {
			return err
		}
	}
	// If the hash changed, update the event
	if infos.Banner == hash {
		return nil
	}
	infos.Banner = hash

	blob, err := json.Marshal(infos)
	if err != nil {
		return err
	}
	return b.database.Put(append(dbJoinedEventPrefix, event...), blob, nil)
}
