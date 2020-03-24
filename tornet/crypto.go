// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2019 Péter Szilágyi. All rights reserved.

package tornet

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"time"

	"github.com/cretz/bine/torutil/ed25519"
	"golang.org/x/crypto/sha3"
)

// SecretIdentity is a tuple consisting of a permanent certificate identifying the
// local user and a semi-permanent private key identifying the Tor onion address.
//
// The onion key should not be changed frequently, as it prevents trusted peers
// from connecting to the local node. A successful dial will however share the
// updated public key. Alternatively, extra-protocol exchanges can also be used
// to notify peers of the change.
type SecretIdentity struct {
	owner tls.Certificate
	onion ed25519.PrivateKey
}

// GenerateIdentity creates a new random local identity by creating a TLS
// certificate for the permanent id and an ed25519 key for the ephemeral onion.
func GenerateIdentity() (*SecretIdentity, error) {
	// Generate a private key for the permanent certificate
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	blob, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, err
	}
	pemPriv := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: blob})

	// Generate the self-signed permanent certificate
	template := x509.Certificate{
		SerialNumber: new(big.Int),              // Nice, complicated, globally "unique" serial number
		DNSNames:     []string{"localhost"},     // We're connecting through Tor, everything is localhost
		NotAfter:     time.Unix(31415926535, 0), // Permanent id, never expire
	}
	blob, err = x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, err
	}
	pemCert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: blob})

	cert, err := tls.X509KeyPair(pemCert, pemPriv)
	if err != nil {
		return nil, err
	}
	// Generate a private key for the ephemeral onion service
	key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	// Assemble and return the new local identity
	return &SecretIdentity{
		owner: cert,
		onion: key.PrivateKey(),
	}, nil
}

// MarshalJSON implements the json.Marshaller interface, encoding the entire secret
// identity into an unsecured blob.
func (id *SecretIdentity) MarshalJSON() ([]byte, error) {
	key, _ := x509.MarshalECPrivateKey(id.owner.PrivateKey.(*ecdsa.PrivateKey))

	return json.Marshal(map[string][]byte{
		"key":   key,
		"cert":  id.owner.Certificate[0],
		"onion": id.onion,
	})
}

// UnmarshalJSON implements the json.Unmarshaller interface, decoding the entire
// secret identity from an unsecured blob.
func (id *SecretIdentity) UnmarshalJSON(data []byte) error {
	// Retrieve all the components and do baseline sanity checks
	fields := make(map[string][]byte)
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	switch {
	case fields["key"] == nil:
		return errors.New("missing permanent key")
	case fields["cert"] == nil:
		return errors.New("missing permanent certificate")
	case fields["onion"] == nil:
		return errors.New("missing ephemeral key")
	}
	// Parse the certificate and assemble the identity
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: fields["key"]})
	pemCert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: fields["cert"]})

	owner, err := tls.X509KeyPair(pemCert, pemKey)
	if err != nil {
		return err
	}
	id.owner, id.onion = owner, fields["onion"]
	return nil
}

// Public creates a public identity from the secret one that can be shared with
// trusted third parties.
func (id *SecretIdentity) Public() *PublicIdentity {
	return &PublicIdentity{
		owner: id.owner.Certificate[0],
		onion: id.onion.PublicKey(),
	}
}

// cert retrieves the certificate to authenticate the local peer with.
func (id *SecretIdentity) cert() *x509.Certificate {
	cert, _ := x509.ParseCertificate(id.owner.Certificate[0])
	return cert
}

// trash destroys the old onion identity and generates a branch new one in its
// place, preventing any peer without a side channel to retrieve the new one.
func (id *SecretIdentity) trash() error {
	key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	id.onion = key.PrivateKey()
	return nil
}

// PublicIdentity is a tuple consisting of a permanent certificate identifying the
// remote user and a semi-permanent public key identifying the Tor onion address.
//
// The onion key is assumed stable, but it will occasionally be changed. Upon a
// successful dial to a remote peer, the remote identity will be updated based on
// the protocol handshake.
type PublicIdentity struct {
	owner []byte
	onion ed25519.PublicKey
}

// ID returns a short, globally unique identifier for this public key. Essentially
// it is the SHA3 hash of the owner certificate in hexadecimal form.
func (id *PublicIdentity) ID() string {
	hash := sha3.Sum256(id.owner)
	return hex.EncodeToString(hash[:])
}

// MarshalJSON implements the json.Marshaller interface, encoding the entire public
// identity into an unsecured blob.
func (id *PublicIdentity) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string][]byte{
		"cert":  id.owner,
		"onion": id.onion,
	})
}

// UnmarshalJSON implements the json.Unmarshaller interface, decoding the entire
// public identity from an unsecured blob.
func (id *PublicIdentity) UnmarshalJSON(data []byte) error {
	// Retrieve all the components and do baseline sanity checks
	fields := make(map[string][]byte)
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	switch {
	case fields["cert"] == nil:
		return errors.New("missing permanent certificate")
	case fields["onion"] == nil:
		return errors.New("missing ephemeral key")
	}
	// Parse the certificate and assemble the identity
	if _, err := x509.ParseCertificate(fields["cert"]); err != nil {
		return err
	}
	id.owner, id.onion = fields["cert"], fields["onion"]
	return nil
}

// cert retrieves the certificate to authenticate a remote peer with.
func (id *PublicIdentity) cert() *x509.Certificate {
	cert, _ := x509.ParseCertificate(id.owner)
	return cert
}
