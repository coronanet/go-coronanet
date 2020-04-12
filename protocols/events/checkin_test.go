// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package events

import (
	"fmt"
	"testing"
	"time"

	"github.com/coronanet/go-coronanet/tornet"
	"github.com/ethereum/go-ethereum/log"
)

// testHost is a mock host to test interacting with a single hosted event.
type testHost struct {
	event  *Server
	update chan *ServerInfos

	inited chan struct{} // Barrier to wait until the server is assigned
}

func newTestHost() *testHost {
	return &testHost{
		update: make(chan *ServerInfos, 1),
		inited: make(chan struct{}),
	}
}

func (h *testHost) Banner(event tornet.IdentityFingerprint, server *Server) []byte {
	return []byte("steak.jpg")
}

func (h *testHost) OnUpdate(event tornet.IdentityFingerprint, server *Server) {
	<-h.inited
	h.update <- h.event.Infos()
}

func (h *testHost) OnReport(event tornet.IdentityFingerprint, server *Server, pseudonym tornet.IdentityFingerprint, message string) error {
	panic("not implemented)")
}

// testGuest is a mock guest to test interacting with a single joined event.
type testGuest struct {
	event  *Client
	update chan *ClientInfos // Notification channel when the event status changes
	banner chan []byte       // Notification channel when the event banner changes

	inited chan struct{} // Barrier to wait until the client is assigned
}

func newTestGuest() *testGuest {
	return &testGuest{
		update: make(chan *ClientInfos, 1),
		banner: make(chan []byte, 1),
		inited: make(chan struct{}),
	}
}

func (g *testGuest) Status(start, end time.Time) (id tornet.SecretIdentity, name string, status string, message string) {
	return nil, "", "", ""
}

func (g *testGuest) OnUpdate(event tornet.IdentityFingerprint, client *Client) {
	<-g.inited
	g.update <- g.event.Infos()
}

func (g *testGuest) OnBanner(event tornet.IdentityFingerprint, banner []byte) {
	<-g.inited
	g.banner <- banner
}

// Tests the creation of a new event server and client and running the initial
// checkin and metadata exchanges.
func TestCheckin(t *testing.T) {
	t.Parallel()

	var (
		gateway = tornet.NewMockGateway()
		host    = newTestHost()
		guest   = newTestGuest()
	)
	// Create an event server to check into
	server, err := CreateServer(host, gateway, "barbecue", [32]byte{3, 1, 4}, log.Root())
	if err != nil {
		t.Fatalf("failed to create event server: %v", err)
	}
	defer server.Close()

	host.event = server
	close(host.inited)

	// Attach to the server with an event client
	session, err := server.Checkin()
	if err != nil {
		t.Fatalf("failed to create checkin session: %v", err)
	}
	client, err := CreateClient(guest, gateway, session.Identity, session.Address, session.Auth, log.Root())
	if err != nil {
		t.Fatalf("failed to create event client: %v", err)
	}
	defer client.Close()

	guest.event = client
	close(guest.inited)

	// Ensure that the guest appears in the server's participant list
	serverInfos := <-host.update
	if _, ok := serverInfos.Participants[client.infos.Pseudonym.Fingerprint()]; !ok {
		t.Errorf("client missing from participant list")
	}
	// Ensure the event metadata appears in the client's infos
	<-guest.update // metadata + status race, we check only the combo result (2nd)

	clientInfos := <-guest.update
	if clientInfos.Name != "barbecue" {
		t.Errorf("event name mismatch: have %s, want %s", clientInfos.Name, "barbecue")
	}
	if clientInfos.Attendees != 2 { // self + organizer
		t.Errorf("event attendees count mismatch: have %d, want %d", clientInfos.Attendees, 2)
	}
	if clientInfos.Negatives != 0 {
		t.Errorf("event negatives count mismatch: have %d, want %d", clientInfos.Negatives, 0)
	}
	if clientInfos.Suspected != 0 {
		t.Errorf("event suspected count mismatch: have %d, want %d", clientInfos.Suspected, 0)
	}
	if clientInfos.Positives != 0 {
		t.Errorf("event positives count mismatch: have %d, want %d", clientInfos.Positives, 0)
	}
}

// Tests that once an authentication credential is used up for checking in, no
// subsequent connections can be made with it.
func TestDuplicateCheckin(t *testing.T) {
	t.Parallel()

	var (
		gateway = tornet.NewMockGateway()
		host    = newTestHost()
		guest   = newTestGuest()
	)
	// Create an event server to check into
	server, err := CreateServer(host, gateway, "barbecue", [32]byte{3, 1, 4}, log.Root())
	if err != nil {
		t.Fatalf("failed to create event server: %v", err)
	}
	defer server.Close()

	host.event = server
	close(host.inited)

	// Attach to the server with an event client
	session, err := server.Checkin()
	if err != nil {
		t.Fatalf("failed to create checkin session: %v", err)
	}
	client, err := CreateClient(guest, gateway, session.Identity, session.Address, session.Auth, log.Root())
	if err != nil {
		t.Fatalf("failed to create event client: %v", err)
	}
	defer client.Close()

	guest.event = client
	close(guest.inited)

	// Consume the server and client events to ensure nothing's left in the system
	<-host.update
	<-guest.update
	<-guest.update
	<-guest.banner

	// Attempt to connect with a malicious guest reusing the same auth credentials
	if _, err := CreateClient(newTestGuest(), gateway, session.Identity, session.Address, session.Auth, log.Root()); err == nil {
		t.Fatalf("duplicate checkin permitted")
	}
}

// Tests that once an authentication credential is used up for checking in, a new
// one can be generated in its place which can be used to check in.
func TestSubsequentCheckin(t *testing.T) {
	t.Parallel()

	var (
		gateway = tornet.NewMockGateway()
		host    = newTestHost()
	)
	// Create an event server to check into
	server, err := CreateServer(host, gateway, "barbecue", [32]byte{3, 1, 4}, log.Root())
	if err != nil {
		t.Fatalf("failed to create event server: %v", err)
	}
	defer server.Close()

	host.event = server
	close(host.inited)

	// Attach to the server with an event client
	session, err := server.Checkin()
	if err != nil {
		t.Fatalf("failed to create first checkin session: %v", err)
	}
	firstGuest := newTestGuest()
	firstClient, err := CreateClient(firstGuest, gateway, session.Identity, session.Address, session.Auth, log.Root())
	if err != nil {
		t.Fatalf("failed to create first event client: %v", err)
	}
	defer firstClient.Close()

	firstGuest.event = firstClient
	close(firstGuest.inited)

	// Consume the server and client events to ensure nothing's left in the system
	<-host.update
	<-firstGuest.update
	<-firstGuest.update
	<-firstGuest.banner

	// Attempt to connect with a second guest, using new checkin credentials
	session, err = server.Checkin()
	if err != nil {
		t.Fatalf("failed to create second checkin session: %v", err)
	}
	secondGuest := newTestGuest()
	secondClient, err := CreateClient(secondGuest, gateway, session.Identity, session.Address, session.Auth, log.Root())
	if err != nil {
		t.Fatalf("failed to create second event client: %v", err)
	}
	defer secondClient.Close()

	secondGuest.event = secondClient
	close(secondGuest.inited)

	// Ensure both server and second guest fire events
	<-host.update
	<-secondGuest.update
	<-secondGuest.update
	<-secondGuest.banner
}

// Tests that multiple concurrent checkins can be in process at the same time.
func TestConcurrentCheckin(t *testing.T) {
	t.Parallel()

	var (
		gateway = tornet.NewMockGateway()
		host    = newTestHost()
	)
	// Create an event server to check into
	server, err := CreateServer(host, gateway, "barbecue", [32]byte{3, 1, 4}, log.Root())
	if err != nil {
		t.Fatalf("failed to create event server: %v", err)
	}
	defer server.Close()

	host.event = server
	close(host.inited)

	// Create two concurrent checkin sessions
	firstSession, err := server.Checkin()
	if err != nil {
		t.Fatalf("failed to create first checkin session: %v", err)
	}
	secondSession, err := server.Checkin()
	if err != nil {
		t.Fatalf("failed to create second checkin session: %v", err)
	}
	// Concurrently run two clients and wait for both
	errc := make(chan error, 2)
	for _, session := range []*CheckinSession{firstSession, secondSession} {
		go func(session *CheckinSession) {
			guest := newTestGuest()
			client, err := CreateClient(guest, gateway, session.Identity, session.Address, session.Auth, log.Root())
			if err != nil {
				errc <- err
			}
			defer client.Close()

			guest.event = client
			close(guest.inited)

			<-host.update
			<-guest.update
			<-guest.update
			<-guest.banner
			errc <- nil
		}(session)
	}
	for i := 0; i < 2; i++ {
		if err := <-errc; err != nil {
			fmt.Errorf("client checking failed: %v", err)
		}
	}
}

// Tests that once an event is concluded, the checkin mechanism gets disabled.
func TestPostTerminationCheckin(t *testing.T) {
	t.Parallel()

	gateway := tornet.NewMockGateway()

	// Create an event server to check into, retrieve it's checkin credentials and
	// terminate it.
	server, err := CreateServer(newTestHost(), gateway, "barbecue", [32]byte{3, 1, 4}, log.Root())
	if err != nil {
		t.Fatalf("failed to create event server: %v", err)
	}
	session, err := server.Checkin()
	if err != nil {
		t.Fatalf("failed to create checkin session: %v", err)
	}
	server.Terminate()

	// Attempt to check in with the old credentials and ensure it fails
	if _, err := CreateClient(newTestGuest(), gateway, session.Identity, session.Address, session.Auth, log.Root()); err == nil {
		t.Fatalf("post-termination checkin permitted")
	}
	// Restart the server to ensure a reboot doesn't re-enable checkin
	infos := server.Infos()
	server.Close()

	server, err = RecreateServer(newTestHost(), gateway, infos, log.Root())
	if err != nil {
		t.Fatalf("failed to recreate event server: %v", err)
	}
	defer server.Close()

	if _, err := server.Checkin(); err == nil {
		t.Fatalf("recreated server reopened checkin")
	}
}
