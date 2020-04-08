// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package events

import (
	"bytes"
	"testing"
	"time"

	"github.com/coronanet/go-coronanet/tornet"
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

func (h *testHost) OnUpdate(event tornet.IdentityFingerprint) {
	<-h.inited
	h.update <- h.event.Infos()
}

func (h *testHost) OnReport(event tornet.IdentityFingerprint, pseudonym tornet.IdentityFingerprint, message string) error {
	panic("not implemented)")
}

// testGuest is a mock guest to test interacting with a single joined event.
type testGuest struct {
	event  *Client
	update chan *ClientInfos

	inited chan struct{} // Barrier to wait until the client is assigned
}

func newTestGuest() *testGuest {
	return &testGuest{
		update: make(chan *ClientInfos, 1),
		inited: make(chan struct{}),
	}
}

func (g *testGuest) Status(start, end time.Time) (id tornet.SecretIdentity, name string, status string, message string) {
	return nil, "", "", ""
}

func (g *testGuest) OnUpdate(event tornet.IdentityFingerprint) {
	<-g.inited
	g.update <- g.event.Infos()
}

// Tests the creation of a new event server and client and running the initial
// checkin and metadata exchanges.
func TestCheckin(t *testing.T) {
	var (
		gateway = tornet.NewMockGateway()
		host    = newTestHost()
		guest   = newTestGuest()
	)
	// Create an event server to check into
	server, err := CreateServer(host, gateway, "barbecue", []byte("steak.jpg"))
	if err != nil {
		t.Fatalf("failed to create event server: %v", err)
	}
	defer server.Close()

	host.event = server
	close(host.inited)

	// Attach to the server with an event client
	client, err := CreateClient(guest, gateway, server.infos.Identity.Public(), server.infos.Address.Public(), server.checkin)
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
	if !bytes.Equal(clientInfos.Banner, []byte("steak.jpg")) {
		t.Errorf("event banner mismatch: have %s, want %s", clientInfos.Banner, "steak.jpg")
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
	var (
		gateway = tornet.NewMockGateway()
		host    = newTestHost()
		guest   = newTestGuest()
	)
	// Create an event server to check into
	server, err := CreateServer(host, gateway, "barbecue", []byte("steak.jpg"))
	if err != nil {
		t.Fatalf("failed to create event server: %v", err)
	}
	defer server.Close()

	host.event = server
	close(host.inited)

	// Attach to the server with an event client
	checkin := server.checkin // save for duplication attack

	client, err := CreateClient(guest, gateway, server.infos.Identity.Public(), server.infos.Address.Public(), checkin)
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

	// Attempt to connect with a malicious guest reusing the same auth credentials
	if _, err := CreateClient(newTestGuest(), gateway, server.infos.Identity.Public(), server.infos.Address.Public(), checkin); err == nil {
		t.Fatalf("duplicate checkin permitted")
	}
}

// Tests that once an authentication credential is used up for checking in, a new
// one is generated in its place which can be used to check in.
func TestSubsequentCheckin(t *testing.T) {
	var (
		gateway = tornet.NewMockGateway()
		host    = newTestHost()
	)
	// Create an event server to check into
	server, err := CreateServer(host, gateway, "barbecue", []byte("steak.jpg"))
	if err != nil {
		t.Fatalf("failed to create event server: %v", err)
	}
	defer server.Close()

	host.event = server
	close(host.inited)

	// Attach to the server with an event client
	firstGuest := newTestGuest()

	firstClient, err := CreateClient(firstGuest, gateway, server.infos.Identity.Public(), server.infos.Address.Public(), server.checkin)
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

	// Attempt to connect with a second guest, using new checkin credentials
	secondGuest := newTestGuest()

	secondClient, err := CreateClient(secondGuest, gateway, server.infos.Identity.Public(), server.infos.Address.Public(), server.checkin)
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
}
