// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package tornet

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"

	"github.com/cretz/bine/tor"
	"github.com/cretz/bine/torutil"
	tored25519 "github.com/cretz/bine/torutil/ed25519"
	"github.com/ethereum/go-ethereum/log"
)

// ServerConfig can be used to fine tune the initial setup of a tornet server.
type ServerConfig struct {
	Gateway  Gateway        // Tor gateway to open the listener through
	Address  SecretAddress  // Address private key to listen through
	Identity SecretIdentity // Identity private key to encrypt traffic with
	PeerSet  *PeerSet       // Connection de-duplicator and handler

	Logger log.Logger // Logger to allow injecting pre-networking context
}

// Server is a network entity of a decentralized overlay network fully deployed
// on top of the Tor network. It listens on inbound connection on an specified
// onion address and will only accept authorized clients.
type Server struct {
	listener net.Listener // TLS wrapped onion for inbound connections
	listQuit chan error   // Termination channel for the listener goroutine
	logger   log.Logger   // Logger to help trace connections
}

// NewServer creates tornet server, seeding it with a secret identity and an
// initial set of trusted remote peers.
func NewServer(config ServerConfig) (*Server, error) {
	// Create the server wrapper to manage the dynamic authentications
	server := &Server{
		listQuit: make(chan error),
	}
	// Create the onion on Tor to release the address private key
	onion, err := config.Gateway.Listen(context.Background(), &tor.ListenConf{
		Key:         tored25519.FromCryptoPrivateKey(ed25519.NewKeyFromSeed(config.Address)).PrivateKey(),
		RemotePorts: []int{1},
		Version3:    true,
		NoWait:      true, // DO NOT CONNECT TOR ON YOUR OWN
	})
	if err != nil {
		return nil, err
	}
	logger := config.Logger
	if logger == nil {
		logger = log.Root()
	}
	server.logger = logger.New("onion", onion.Addr().String())

	// Wrap the onion service into a TLS stream as we don't much trust Tor to be
	// the only encryption layer in the protocol. The listener configuration is
	// deliberately a bit complicated. Instead of pre-injecting authenticated
	// certificates we validate on the fly by cross checking a public key ring.
	server.listener = tls.NewListener(onion, &tls.Config{
		// Certificates ensures that the secret identity is the only thing we're
		// willing to talk through.
		Certificates: []tls.Certificate{config.Identity.certificate()},

		// ClientAuth ensures that the client uses certificates for authentication
		// too, but we don't want to validate it automatically, rather manually.
		ClientAuth: tls.RequireAnyClientCert,

		// VerifyPeerCertificate is the actual client certification validation
		VerifyPeerCertificate: func(certificates [][]byte, _ [][]*x509.Certificate) error {
			// We know we have at least one certificate courtesy of `ClientAuth`,
			// and we don't care about anyone sending more than one.
			cert, err := x509.ParseCertificate(certificates[0])
			if err != nil {
				return err
			}
			// We only use Ed25519 curves, discard any connections not speaking it
			pub, ok := cert.PublicKey.(ed25519.PublicKey)
			if !ok {
				return errors.New("invalid public key type")
			}
			// The certificate has the right crypto, authenticate the public key
			// against the local key ring.
			uid := PublicIdentity(pub).Fingerprint()

			config.PeerSet.lock.RLock()
			_, authorized := config.PeerSet.auths[uid]
			config.PeerSet.lock.RUnlock()

			if !authorized {
				return fmt.Errorf("unauthorized public key: %s", uid)
			}
			// Public key authorized, validate the self-signed certificate
			return cert.CheckSignature(cert.SignatureAlgorithm, cert.RawTBSCertificate, cert.Signature)
		},
	})
	go server.loop(config.PeerSet)

	return server, nil
}

// loop keeps accepting network connections until it's torn down.
func (s *Server) loop(peerset *PeerSet) {
	// Loop until accept fails (typically the server is closed)
	s.logger.Info("Tornet server listening")

	var err error
	for err == nil {
		var conn net.Conn
		if conn, err = s.listener.Accept(); err == nil {
			go peerset.handle(conn, make(chan error, 1)) // We don't care about the error
		}
	}
	// Something went wrong, terminate
	s.logger.Info("Tornet server terminating", "err", err)
	s.listQuit <- err
}

// Close terminates the server's listener socket, drops all live connections and
// returns.
func (s *Server) Close() error {
	var errs []error
	if err := s.listener.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := <-s.listQuit; err != nil {
		errs = append(errs, err)
	}
	switch {
	case errs == nil:
		return nil
	case len(errs) == 1:
		return errs[0]
	default:
		return fmt.Errorf("%v", errs) // Ugh
	}
}

// DialConfig can be used to fine tune the dialing tornet process.
type DialConfig struct {
	Gateway  Gateway        // Tor gateway to dial through
	Address  PublicAddress  // Address public key to connect to
	Server   PublicIdentity // Server public key to authenticate
	Identity SecretIdentity // Private key to encrypt traffic with
	PeerSet  *PeerSet       // Connection de-duplicator and handler
}

// DialServer attempts to connect to a remote server at the specified address,
// and if successful, the method does a bidirectional TLS handshake. If all is
// ok, the peer set's internal handler will be run.
//
// Since the handshake is async, a failure cannot be immediately returned. Instead,
// an error channel is returned which will get sent any failure after dialing.
func DialServer(ctx context.Context, config DialConfig) (chan error, error) {
	// Try to establish a connection through the Tor network
	dialer, err := config.Gateway.Dialer(ctx, &tor.DialConf{
		SkipEnableNetwork: true, // DO NOT CONNECT TOR ON YOUR OWN
	})
	if err != nil {
		return nil, err
	}
	onion := torutil.OnionServiceIDFromPublicKey(tored25519.FromCryptoPublicKey(ed25519.PublicKey(config.Address)))
	conn, err := dialer.Dial("tcp", fmt.Sprintf("%s.onion:1", onion))
	if err != nil {
		return nil, err
	}
	// Wrap the connection into a TLS client to ensure mutual authentication
	done := make(chan error, 1) // TODO(karalabe): Bleah, this is one ugly hack

	go config.PeerSet.handle(tls.Client(conn, &tls.Config{
		// Certificates ensures that the secret identity is the only thing we're
		// willing to talk through.
		Certificates: []tls.Certificate{config.Identity.certificate()},

		// InsecureSkipVerify skips all the baked in validations and lets us run
		// our own fancy magic.
		InsecureSkipVerify: true,

		// VerifyPeerCertificate is the actual client certification validation
		VerifyPeerCertificate: func(certificates [][]byte, _ [][]*x509.Certificate) error {
			// We know we have at least one certificate since we're initiating a
			// TLS session and the server must authenticate itself.
			cert, err := x509.ParseCertificate(certificates[0])
			if err != nil {
				return err
			}
			// We only use Ed25519 curves, discard any connections not speaking it
			pub, ok := cert.PublicKey.(ed25519.PublicKey)
			if !ok {
				return errors.New("invalid public key type")
			}
			// The certificate has the right crypto, authenticate it
			if !bytes.Equal(pub, config.Server) {
				return errors.New("unexpected server key")
			}
			// Double check against the local keyring, don't permit insecure connections
			uid := PublicIdentity(pub).Fingerprint()

			config.PeerSet.lock.RLock()
			_, authorized := config.PeerSet.auths[uid]
			config.PeerSet.lock.RUnlock()

			if !authorized {
				return errors.New("unauthorized public key")
			}
			// Public key authorized, validate the self-signed certificate
			return cert.CheckSignature(cert.SignatureAlgorithm, cert.RawTBSCertificate, cert.Signature)
		},
	}), done)
	return done, nil
}
