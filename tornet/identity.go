// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package tornet

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"time"

	"golang.org/x/crypto/sha3"
)

// SecretIdentity is a permanent Ed25519 private key identifying the local user.
type SecretIdentity []byte

// PublicIdentity is a permanent Ed25519 public key identifying a remote user.
type PublicIdentity []byte

// IdentityFingerprint is a universally unique identifier for a secret identity.
type IdentityFingerprint string

// SecretAddress is a stable Ed25519 private key identifying a Tor onion address
// the local user would be listening on. Users will periodically rotate these.
type SecretAddress []byte

// PublicAddress is a stable Ed25519 public key identifying a Tor onion address
// a remote user would be listening on. Users will periodically rotate these.
type PublicAddress []byte

// AddressFingerprint is a universally unique identifier for a Tor onion address.
type AddressFingerprint string

// GenerateIdentity creates a new random local cryptographic identity.
func GenerateIdentity() (SecretIdentity, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return SecretIdentity(priv.Seed()), nil
}

// Public generates and returns the public identity from a secret one.
//
// Note, this method is heavy. Cache it.
func (id SecretIdentity) Public() PublicIdentity {
	return PublicIdentity(ed25519.NewKeyFromSeed(id).Public().(ed25519.PublicKey))
}

// Fingerprint generates a universally unique identifier for a secret identity.
// Although the unique id is binary, it's returned base64 encoded to avoid weird
// codec issues in JSON and HTTP.
//
// Note, this method is heavy. Cache it.
func (id SecretIdentity) Fingerprint() IdentityFingerprint {
	return id.Public().Fingerprint()
}

// Fingerprint generates a universally unique identifier for a public identity.
// Although the unique id is binary, it's returned base64 encoded to avoid weird
// codec issues in JSON and HTTP.
//
// Note, this method is heavy. Cache it.
func (id PublicIdentity) Fingerprint() IdentityFingerprint {
	hash := sha3.Sum256(id)
	return IdentityFingerprint(base64.RawURLEncoding.EncodeToString(hash[:]))
}

// certificate generates a deterministic server side TLS certificate from the
// secret identity.
//
// Note, this method is heavy. Only call it once on startup and cache it.
func (id SecretIdentity) certificate() tls.Certificate {
	// Generate the certificate key
	priv := ed25519.NewKeyFromSeed(id)

	blob, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		panic(err)
	}
	pemPriv := pem.EncodeToMemory(&pem.Block{Type: "ES PRIVATE KEY", Bytes: blob})

	// Generate the self-signed permanent certificate
	template := x509.Certificate{
		SerialNumber: new(big.Int),              // Nice, complicated, universally "unique" serial number
		DNSNames:     []string{"localhost"},     // We're connecting through Tor, everything is localhost
		NotAfter:     time.Unix(31415926535, 0), // Permanent id, never expire
	}
	blob, err = x509.CreateCertificate(nil, &template, &template, priv.Public(), priv)
	if err != nil {
		panic(err)
	}
	pemCert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: blob})

	cert, err := tls.X509KeyPair(pemCert, pemPriv)
	if err != nil {
		panic(err)
	}
	return cert
}

// GenerateAddress creates a new random cryptographic onion address.
func GenerateAddress() (SecretAddress, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return SecretAddress(priv.Seed()), nil
}

// Public generates and returns the public address from a secret one.
//
// Note, this method is heavy. Cache it.
func (addr SecretAddress) Public() PublicAddress {
	return PublicAddress(ed25519.NewKeyFromSeed(addr).Public().(ed25519.PublicKey))
}

// Fingerprint generates a universally unique identifier for a secret address.
// Although the unique id is binary, it's returned base64 encoded to avoid weird
// codec issues in JSON and HTTP.
//
// Note, this method is heavy. Cache it.
func (addr SecretAddress) Fingerprint() AddressFingerprint {
	return addr.Public().Fingerprint()
}

// Fingerprint generates a universally unique identifier for a public address.
// Although the unique id is binary, it's returned base64 encoded to avoid weird
// codec issues in JSON and HTTP.
//
// Note, this method is heavy. Cache it.
func (addr PublicAddress) Fingerprint() AddressFingerprint {
	hash := sha3.Sum256(addr)
	return AddressFingerprint(base64.RawURLEncoding.EncodeToString(hash[:]))
}
