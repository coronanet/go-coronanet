// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package main

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/coronanet/go-coronanet/rest"
)

// Tests basic lifecycle operations around the local user account: creation,
// updating, deletion and various combinations and sequences of these.
func TestProfileLifecycle(t *testing.T) {
	t.Parallel()

	alice, _ := newTestNode("", "--verbosity", "5")
	defer alice.close()

	// Ensure there's no existing profile on a new node
	if _, err := alice.Profile(); err == nil {
		t.Fatalf("profile exists on new node")
	}
	// Create a profile and check name updating
	if err := alice.CreateProfile(); err != nil {
		t.Fatalf("failed to create profile: %v", err)
	}
	if infos, err := alice.Profile(); err != nil {
		t.Fatalf("failed to retrieve initial profile: %v", err)
	} else if infos.Name != "" {
		t.Fatalf("non empty name on initial profile: %s", infos.Name)
	}
	if err := alice.UpdateProfile(&rest.ProfileInfos{Name: "Alice"}); err != nil {
		t.Fatalf("failed to update profile infos: %v", err)
	}
	if infos, err := alice.Profile(); err != nil {
		t.Fatalf("failed to retrieve updated profile: %v", err)
	} else if infos.Name != "Alice" {
		t.Fatalf("name mismatch on updated profile: have %s, want Alice", infos.Name)
	}
	// Duplicate updates should not be an issue
	if err := alice.UpdateProfile(&rest.ProfileInfos{Name: "Alice"}); err != nil {
		t.Fatalf("failed to re-update profile infos: %v", err)
	}
	if infos, err := alice.Profile(); err != nil {
		t.Fatalf("failed to retrieve re-updated profile: %v", err)
	} else if infos.Name != "Alice" {
		t.Fatalf("name mismatch on re-updated profile: have %s, want Alice", infos.Name)
	}
	// Duplicate creates should be forbidden
	if err := alice.CreateProfile(); err == nil {
		t.Fatalf("allowed to recreate profile")
	}
	// Profile deletion should remove all data and forbid updating
	if err := alice.DeleteProfile(); err != nil {
		t.Fatalf("profile deletion failed: %v", err)
	}
	if _, err := alice.Profile(); err == nil {
		t.Fatalf("profile exists after deletion")
	}
	if err := alice.UpdateProfile(&rest.ProfileInfos{Name: "Alice"}); err == nil {
		t.Fatalf("allowed to update deleted profile")
	}
	// Duplicate deletion should be fine
	if err := alice.DeleteProfile(); err != nil {
		t.Fatalf("duplicate profile deletion failed: %v", err)
	}
	// Profile should be allowed to be recreated
	if err := alice.CreateProfile(); err != nil {
		t.Fatalf("failed to recreate profile: %v", err)
	}
	if infos, err := alice.Profile(); err != nil {
		t.Fatalf("failed to retrieve recreated profile: %v", err)
	} else if infos.Name != "" {
		t.Fatalf("non empty name on recreated profile: %s", infos.Name)
	}
}

// Tests that a previously create profile can be reloaded on reboot.
func TestProfileReloading(t *testing.T) {
	t.Parallel()

	// Create a persistent datadir managed by the test
	datadir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("failed to create persistend datadir: %v", err)
	}
	defer os.RemoveAll(datadir)

	// Create a new node, create a user and shut it down
	alice, _ := newTestNode(datadir, "--verbosity", "5")
	alice.CreateProfile()
	alice.UpdateProfile(&rest.ProfileInfos{Name: "Alice"})
	alice.close()

	// Recreate the node and check that the profile is still there
	alice, _ = newTestNode(datadir, "--verbosity", "5")
	defer alice.close()

	if infos, err := alice.Profile(); err != nil {
		t.Fatalf("failed to retrieve profile: %v", err)
	} else if infos.Name != "Alice" {
		t.Fatalf("name mismatch on profile: have %s, want Alice", infos.Name)
	}
}
