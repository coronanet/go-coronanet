// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2019 Péter Szilágyi. All rights reserved.

package tornet

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"testing"
	"time"

	"github.com/cretz/bine/tor"
	"github.com/ipsn/go-libtor"
)

// Tests that the node can use the Tor gateway for real connectivity. This is a
// very expensive test so all others mock Tor out and use localhost connections.
func TestNodeTorConnectivity(t *testing.T) {
	// Create the global Tor proxy
	proxy, err := tor.Start(nil, &tor.StartConf{ProcessCreator: libtor.Creator, UseEmbeddedControlConn: true})
	if err != nil {
		t.Fatalf("Failed to create global tor proxy: %v", err)
	}
	defer proxy.Close()

	// Call the actual connectivity test
	testNodeConnectivity(t, NewTorGateway(proxy))
}

// Tests that the node can use the mock (nil) Tor gateway for fake connectivity.
func TestNodeMockConnectivity(t *testing.T) {
	testNodeConnectivity(t, NewMockGateway())
}

func testNodeConnectivity(t *testing.T, gateway Gateway) {
	// Create the identities for two users
	id1, _ := GenerateIdentity()
	id2, _ := GenerateIdentity()

	// Create the peer handlers that just sleep a bit
	handler := func(id string, conn net.Conn) error {
		_, err := ioutil.ReadAll(conn)
		return err
	}
	// Create and boot the mutually trusting nodes
	node1 := New(gateway, id1, map[string]*PublicIdentity{"node2": id2.Public()}, handler)
	if err := node1.Start(); err != nil {
		t.Fatalf("Failed to start first node: %v", err)
	}
	defer node1.Stop()

	node2 := New(gateway, id2, map[string]*PublicIdentity{"node1": id1.Public()}, handler)
	if err := node2.Start(); err != nil {
		t.Fatalf("Failed to start second node: %v", err)
	}
	defer node2.Stop()

	// Wait a while until the nodes have a chance to connect
	for i := 0; i < 60; i++ {
		node1.lock.Lock()
		conns1 := len(node1.conns)
		node1.lock.Unlock()

		node2.lock.Lock()
		conns2 := len(node2.conns)
		node2.lock.Unlock()

		if conns1 == 1 && conns2 == 1 {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("Failed to establish bidirectional channel")
}

// Test that nodes without remote peers don't choke on anything.
func TestNodeEmpty(t *testing.T) {
	id, _ := GenerateIdentity()

	node := New(NewMockGateway(), id, nil, nil)
	if err := node.Start(); err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer node.Stop()
}

// Test that nodes with complex transitive relationships can form the required
// network topology.
func TestNodeMultiConnectivity(t *testing.T) {
	// Create a large number of peers with random trusts and check cross connectivity
	peers := 256

	identities := make([]*SecretIdentity, peers)
	for i := 0; i < peers; i++ {
		identities[i], _ = GenerateIdentity()
	}
	trusts := make([]map[string]*PublicIdentity, peers)
	for i := 0; i < peers; i++ {
		trusts[i] = make(map[string]*PublicIdentity)
	}
	for i := 0; i < peers; i++ {
		// For every node, generate a set of trusted peers
		trust := rand.Intn(32)
		for len(trusts[i]) < trust {
			// Trust a random peer, make sure we don't duplicate
			idx := rand.Intn(peers)
			if idx == i {
				// Don't pick ourselves
				continue
			}
			id := fmt.Sprintf("node%d", idx)
			if _, ok := trusts[i][id]; ok {
				// Don't pick the same peer twice
				continue
			}
			// Yay, new trust relationship, add mutually
			trusts[i][id] = identities[idx].Public()
			trusts[idx][fmt.Sprintf("node%d", i)] = identities[i].Public()
		}
	}
	// Create a global handler that just idles until connections are torn down
	handler := func(id string, conn net.Conn) error {
		_, err := ioutil.ReadAll(conn)
		return err
	}
	// Boot up a set of nodes, one per peer
	gateway := NewMockGateway()

	nodes := make([]*Node, peers)
	for i := 0; i < peers; i++ {
		nodes[i] = New(gateway, identities[i], trusts[i], handler)
		if err := nodes[i].Start(); err != nil {
			t.Fatalf("Failed to start node #%d: %v", i, err)
		}
		defer nodes[i].Stop()
	}
	// Wait a while until the nodes have a chance to connect
	for i := 0; i < 40; i++ {
		finished := true
		for j := 0; j < peers; j++ {
			nodes[j].lock.Lock()
			conns := len(nodes[j].conns)
			nodes[j].lock.Unlock()

			if conns != len(trusts[j]) {
				finished = false
				break
			}
		}
		if finished {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("Failed to establish required connections")
}

// Test that new peers can be added to nodes after they have been booted up.
func TestNodeTrust(t *testing.T) {
	// Create the identities for two users
	id1, _ := GenerateIdentity()
	id2, _ := GenerateIdentity()

	// Create the peer handlers that just sleep a bit
	handler := func(id string, conn net.Conn) error {
		_, err := ioutil.ReadAll(conn)
		return err
	}
	// Create and boot the non-trusting nodes
	gateway := NewMockGateway()

	node1 := New(gateway, id1, nil, handler)
	if err := node1.Start(); err != nil {
		t.Fatalf("Failed to start first node: %v", err)
	}
	defer node1.Stop()

	node2 := New(gateway, id2, nil, handler)
	if err := node2.Start(); err != nil {
		t.Fatalf("Failed to start second node: %v", err)
	}
	defer node2.Stop()

	// Wait a while and ensure no connection exists between the nodes
	time.Sleep(time.Second)

	node1.lock.Lock()
	conns1 := len(node1.conns)
	node1.lock.Unlock()

	node2.lock.Lock()
	conns2 := len(node2.conns)
	node2.lock.Unlock()

	if conns1 != 0 && conns2 != 0 {
		t.Fatalf("Connection count mismatch: have %d/%d, want 0/0", conns1, conns2)
	}
	// Add a one sided trust. This simulates the onion address and public key of a
	// node leaking out. No connection should be establishable.
	if err := node1.Trust("node2", id2.Public()); err != nil {
		t.Fatalf("Failed to trust second node: %v", err)
	}
	// Wait a while and ensure no connection exists between the nodes
	time.Sleep(time.Second)

	node1.lock.Lock()
	conns1 = len(node1.conns)
	node1.lock.Unlock()

	node2.lock.Lock()
	conns2 = len(node2.conns)
	node2.lock.Unlock()

	if conns1 != 0 && conns2 != 0 {
		t.Fatalf("Connection count mismatch: have %d/%d, want 0/0", conns1, conns2)
	}
	// Add a two sided trust. This should trigger a successful connection.
	if err := node2.Trust("node1", id1.Public()); err != nil {
		t.Fatalf("Failed to trust second node: %v", err)
	}
	for i := 0; i < 10; i++ {
		node1.lock.Lock()
		conns1 = len(node1.conns)
		node1.lock.Unlock()

		node2.lock.Lock()
		conns2 = len(node2.conns)
		node2.lock.Unlock()

		if conns1 == 1 && conns2 == 1 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("Failed to establish bidirectional channel")
}

// Test that revoking an existing trust will break any pending connections, as
// well as change the original onion address.
func TestNodeUntrust(t *testing.T) {
	// Create the identities for three users
	id1, _ := GenerateIdentity()
	id2, _ := GenerateIdentity()
	id3, _ := GenerateIdentity()

	// Create the peer handlers that just sleep a bit
	handler := func(id string, conn net.Conn) error {
		_, err := ioutil.ReadAll(conn)
		return err
	}
	// Create and boot the mutually trusting nodes
	gateway := NewMockGateway()

	node1 := New(gateway, id1, map[string]*PublicIdentity{"node2": id2.Public(), "node3": id3.Public()}, handler)
	if err := node1.Start(); err != nil {
		t.Fatalf("Failed to start first node: %v", err)
	}
	defer node1.Stop()

	node2 := New(gateway, id2, map[string]*PublicIdentity{"node1": id1.Public(), "node3": id3.Public()}, handler)
	if err := node2.Start(); err != nil {
		t.Fatalf("Failed to start second node: %v", err)
	}
	defer node2.Stop()

	node3 := New(gateway, id3, map[string]*PublicIdentity{"node1": id1.Public(), "node2": id2.Public()}, handler)
	if err := node3.Start(); err != nil {
		t.Fatalf("Failed to start third node: %v", err)
	}
	defer node3.Stop()

	// Stash away the onion address of the original node
	onion1 := node1.owner.onion
	onion3 := node3.owner.onion

	// Wait a while until the nodes had a chance to connect
	time.Sleep(time.Second)

	node1.lock.Lock()
	conns1 := len(node1.conns)
	node1.lock.Unlock()

	node2.lock.Lock()
	conns2 := len(node2.conns)
	node2.lock.Unlock()

	node3.lock.Lock()
	conns3 := len(node3.conns)
	node3.lock.Unlock()

	if conns1 != 2 || conns2 != 2 || conns3 != 2 {
		t.Fatalf("Connection count mismatch: have %d/%d/%d, want 2/2/2", conns1, conns2, conns3)
	}
	// Break the trust between 1-2, unidirectionally
	if err := node1.Untrust("node2"); err != nil {
		t.Fatalf("Failed to break trust: %v", err)
	}
	time.Sleep(2 * time.Second)

	node1.lock.Lock()
	conns1 = len(node1.conns)
	node1.lock.Unlock()

	node2.lock.Lock()
	conns2 = len(node2.conns)
	node2.lock.Unlock()

	node3.lock.Lock()
	conns3 = len(node3.conns)
	node3.lock.Unlock()

	if conns1 != 1 || conns2 != 1 || conns3 != 2 {
		t.Fatalf("Connection count mismatch: have %d/%d/%d, want 1/1/2", conns1, conns2, conns3)
	}
	// Verify that the onion address of the untrusting node changed
	if changed := node1.owner.onion; bytes.Equal(changed, onion1) {
		t.Fatalf("First onion address not changed after untrust")
	}
	// Restart all nodes to verify connectivity still works with the new onion. This
	// should propagate the new onion address to any nodes that we still trust.
	node1.Stop()
	node2.Stop()
	node3.Stop()

	if err := node1.Start(); err != nil {
		t.Fatalf("Failed to restart first node: %v", err)
	}
	if err := node2.Start(); err != nil {
		t.Fatalf("Failed to restart second node: %v", err)
	}
	if err := node3.Start(); err != nil {
		t.Fatalf("Failed to restart third node: %v", err)
	}
	time.Sleep(2 * time.Second)

	node1.lock.Lock()
	conns1 = len(node1.conns)
	node1.lock.Unlock()

	node2.lock.Lock()
	conns2 = len(node2.conns)
	node2.lock.Unlock()

	node3.lock.Lock()
	conns3 = len(node3.conns)
	node3.lock.Unlock()

	if conns1 != 1 || conns2 != 1 || conns3 != 2 {
		t.Fatalf("Connection count mismatch: have %d/%d/%d, want 1/1/2", conns1, conns2, conns3)
	}
	// Break the trust between 3-2, unidirectionally
	if err := node3.Untrust("node2"); err != nil {
		t.Fatalf("Failed to break trust: %v", err)
	}
	time.Sleep(2 * time.Second)

	node1.lock.Lock()
	conns1 = len(node1.conns)
	node1.lock.Unlock()

	node2.lock.Lock()
	conns2 = len(node2.conns)
	node2.lock.Unlock()

	node3.lock.Lock()
	conns3 = len(node3.conns)
	node3.lock.Unlock()

	if conns1 != 1 || conns2 != 0 || conns3 != 1 {
		t.Fatalf("Connection count mismatch: have %d/%d/%d, want 1/0/1", conns1, conns2, conns3)
	}
	// Verify that the onion address of the untrusting node changed
	if changed := node3.owner.onion; bytes.Equal(changed, onion3) {
		t.Fatalf("Third onion address not changed after untrust")
	}
	// Restart all nodes to verify connectivity still works with the new onion. If
	// the previously changes onion address of node 1 was not propagated, the two
	// nodes (1 - 3) will not be able to connect any more.
	node1.Stop()
	node2.Stop()
	node3.Stop()

	if err := node1.Start(); err != nil {
		t.Fatalf("Failed to restart first node: %v", err)
	}
	if err := node2.Start(); err != nil {
		t.Fatalf("Failed to restart second node: %v", err)
	}
	if err := node3.Start(); err != nil {
		t.Fatalf("Failed to restart third node: %v", err)
	}
	time.Sleep(2 * time.Second)

	node1.lock.Lock()
	conns1 = len(node1.conns)
	node1.lock.Unlock()

	node2.lock.Lock()
	conns2 = len(node2.conns)
	node2.lock.Unlock()

	node3.lock.Lock()
	conns3 = len(node3.conns)
	node3.lock.Unlock()

	if conns1 != 1 || conns2 != 0 || conns3 != 1 {
		t.Fatalf("Connection count mismatch: have %d/%d/%d, want 1/0/1", conns1, conns2, conns3)
	}
}
