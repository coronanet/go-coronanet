// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package main

import (
	"testing"
	"time"

	"github.com/coronanet/go-coronanet/rest"
)

// Tests that events can be created, updated and terminated.
func TestEventLifecycle(t *testing.T) {
	t.Parallel()

	// Create an event organizer and check that events cannot be hosted without a profile
	alice, _ := newTestNode("", "--verbosity", "5")
	defer alice.close()

	if _, err := alice.CreateEvent(&rest.EventConfig{Name: "Barbecue"}); err == nil {
		t.Fatalf("event created without profile")
	}
	// Create a profile and ensure events can be created now
	alice.CreateProfile()
	alice.UpdateProfile(&rest.ProfileInfos{Name: "Alice"})

	uid, err := alice.CreateEvent(&rest.EventConfig{Name: "Barbecue"})
	if err != nil {
		t.Fatalf("failed to create new event: %v", err)
	}
	// Verify that the event gets tracked by the node
	events, err := alice.HostedEvents()
	if err != nil {
		t.Fatalf("failed to retrieve list of events: %v", err)
	}
	if len(events) != 1 || events[0] != uid {
		t.Fatalf("event list mismatch: have %v, want %v", events, []string{uid})
	}
	stats, err := alice.HostedEvent(uid)
	if err != nil {
		t.Fatalf("failed to retrieve new event: %v", err)
	}
	// Sanity check some basic fields, most will be checked in other tests
	if stats.Name != "Barbecue" {
		t.Fatalf("event name mismatch: have %s, want Barbecuq", stats.Name)
	}
	if stats.End != (time.Time{}) {
		t.Fatalf("new event already terminated: %v", stats.End)
	}
	// Terminate the event and check that is it marked as such
	if err := alice.TerminateEvent(uid); err != nil {
		t.Fatalf("failed to terminate event: %v", err)
	}
	if err := alice.TerminateEvent(uid); err == nil {
		t.Fatalf("duplicate termination permitted")
	}
	stats, err = alice.HostedEvent(uid)
	if err != nil {
		t.Fatalf("failed to retrieve terminated event: %v", err)
	}
	if stats.End == (time.Time{}) {
		t.Fatalf("termination time not persisted")
	}
}

// Tests the checkin mechanism of the event protocol.
func TestEventCheckin(t *testing.T) {
	t.Parallel()

	// Create an event and check that checkin is disallowed without networking
	alice, _ := newTestNode("", "--verbosity", "5", "--hostname", "alice")
	defer alice.close()

	alice.CreateProfile()
	alice.UpdateProfile(&rest.ProfileInfos{Name: "Alice"})

	uid, _ := alice.CreateEvent(&rest.EventConfig{Name: "Barbecue"})
	if _, err := alice.InitEventCheckin(uid); err == nil {
		t.Fatalf("event checkin initiated without network")
	}
	// Enable networking and check that a single checkin session can be created
	alice.EnableGateway()

	secret, err := alice.InitEventCheckin(uid)
	if err != nil {
		t.Fatalf("failed to create checkin session: %v", err)
	}
	retry, err := alice.InitEventCheckin(uid)
	if err != nil {
		t.Fatalf("failed to retrieve existing checkin session: %v", err)
	}
	if secret != retry {
		t.Fatalf("checkin session secret mismatch: have %s, want %s", retry, secret)
	}
	// Create an event participant and ensure checkin fails without profile or networking
	bob, _ := newTestNode("", "--verbosity", "5", "--hostname", "bobby")
	defer bob.close()

	if err := bob.JoinEventCheckin(secret); err == nil {
		t.Fatalf("event checkin joined without profile")
	}
	bob.CreateProfile()
	bob.UpdateProfile(&rest.ProfileInfos{Name: "Bob"})

	if err := bob.JoinEventCheckin(secret); err == nil {
		t.Fatalf("event checkin joined without network")
	}
	// Enable networking and check that event checkin succeeds, once
	bob.EnableGateway()
	if err := bob.JoinEventCheckin(secret); err != nil {
		t.Fatalf("event checkin failed: %v", err)
	}
	if err := bob.JoinEventCheckin(secret); err == nil {
		t.Fatalf("duplicate checkin succeeded")
	}
	if err := alice.WaitEventCheckin(uid); err != nil {
		t.Fatalf("failed to wait for checkin to finish: %v", err)
	}
	// Check that the checked in event is available for querying
	events, err := bob.JoinedEvents()
	if err != nil {
		t.Fatalf("failed to retrieve list of events: %v", err)
	}
	if len(events) != 1 || events[0] != uid {
		t.Fatalf("event list mismatch: have %v, want %v", events, []string{uid})
	}
	if _, err := bob.JoinedEvent(uid); err != nil {
		t.Fatalf("failed to retrieve joined event: %v", err)
	}
	// Check that the old secret cannot be used by someone else
	clair, _ := newTestNode("", "--verbosity", "5", "--hostname", "clair")
	defer clair.close()

	clair.CreateProfile()
	clair.UpdateProfile(&rest.ProfileInfos{Name: "Clair"})
	clair.EnableGateway()

	if err := clair.JoinEventCheckin(secret); err == nil {
		t.Fatalf("checkin succeeded with used credentials")
	}
}
