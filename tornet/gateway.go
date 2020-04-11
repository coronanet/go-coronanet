// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2019 Péter Szilágyi. All rights reserved.

package tornet

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/cretz/bine/tor"
	"github.com/cretz/bine/torutil"
	"github.com/cretz/bine/torutil/ed25519"
	"golang.org/x/net/proxy"
)

// Gateway is an entry point into the Tor network. It supports opening listener
// sockets for incoming connections and creating dialers for outbound ones. Live
// code should use a real Tor object. The purpose of this interface is to also
// provide a mock implementation for testing through localhost.
type Gateway interface {
	// Listen creates an onion service and local listener. The context can be nil.
	Listen(ctx context.Context, conf *tor.ListenConf) (net.Listener, error)

	// Dialer creates a new Dialer for the given configuration. Context can be nil.
	Dialer(ctx context.Context, conf *tor.DialConf) (proxy.Dialer, error)
}

// NewTorGateway creates a new live Tor proxy that passes all network communication
// through the global public Tor network.
func NewTorGateway(proxy *tor.Tor) Gateway {
	return &torGateway{proxy}
}

// torGateway is a live Tor proxy using the global public network.
type torGateway struct {
	proxy *tor.Tor
}

// Listen creates an onion service and local listener. The context can be nil.
func (gw *torGateway) Listen(ctx context.Context, conf *tor.ListenConf) (net.Listener, error) {
	return gw.proxy.Listen(ctx, conf)
}

// Dialer creates a new Dialer for the given configuration. Context can be nil.
func (gw *torGateway) Dialer(ctx context.Context, conf *tor.DialConf) (proxy.Dialer, error) {
	return gw.proxy.Dialer(ctx, conf)
}

// NewMockGateway creates a new mock Tor gateway that short circuits all network
// communication through local in-memory channels.
func NewMockGateway() Gateway {
	return &mockGateway{
		services: make(map[string]net.Listener),
	}
}

// mockGateway simulates a Tor gateway, but short circuits all network channels
// locally via in-memory channels.
type mockGateway struct {
	services map[string]net.Listener // Listeners simulating the global Tor network
	lock     sync.RWMutex            // Lock to make sure concurrent access works
}

// Listen creates an onion service and local listener. The context can be nil.
func (gw *mockGateway) Listen(ctx context.Context, conf *tor.ListenConf) (net.Listener, error) {
	gw.lock.Lock()
	defer gw.lock.Unlock()

	// Assemble the onion URL that we're simulating
	id := torutil.OnionServiceIDFromPublicKey(conf.Key.(ed25519.PrivateKey).PublicKey())
	url := fmt.Sprintf("%s.onion:%d", id, conf.RemotePorts[0])

	if _, ok := gw.services[url]; ok {
		return nil, fmt.Errorf("service %s already open", url)
	}
	// Create a network listener, but leave it to the OS to pick a TCP port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	gw.services[url] = listener

	return &mockGatewayListener{listener, gw, url}, nil
}

// mockGatewayListener is an in-memory listener, which has a hooked close method
// that deregisters the service from the mock gateway.
type mockGatewayListener struct {
	net.Listener // The real TCP listener for network communication

	gateway *mockGateway // Gateway to update on close
	service string       // Onion URL to deregister on close
}

// Close terminates the underlying listener and also removes it from the mock
// gateway service list.
func (l *mockGatewayListener) Close() error {
	l.gateway.lock.Lock()
	defer l.gateway.lock.Unlock()

	delete(l.gateway.services, l.service)
	return l.Listener.Close()
}

// Dialer creates a new Dialer for the given configuration. Context can be nil.
func (gw *mockGateway) Dialer(ctx context.Context, conf *tor.DialConf) (proxy.Dialer, error) {
	return &mockGatewayDialer{gw}, nil
}

// mockGatewayDialer is a dialer that uses the mock listener pool to establish
// network connections.
type mockGatewayDialer struct {
	gateway *mockGateway
}

// Dial connects to the given address via the proxy.
func (d *mockGatewayDialer) Dial(network, addr string) (net.Conn, error) {
	if network != "tcp" {
		return nil, errors.New("unsupported mock protocol")
	}
	d.gateway.lock.RLock()
	defer d.gateway.lock.RUnlock()

	listener := d.gateway.services[addr]
	if listener == nil {
		return nil, errors.New("unknown destination address")
	}
	return net.Dial(listener.Addr().Network(), listener.Addr().String())
}
