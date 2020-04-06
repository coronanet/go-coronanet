# Corona Network Pairing

TODO(karalabe): Extend this section for the final pairing. The current one is a temporary PoC stopgap measure.

## Pairing Protocol v1 (draft)

The purpose of the `pairing` protocol is to be an out-of-bounds secret-exchange between two nodes so they can share their identities and join each other on the main protocol. The wire protocol follows the general mechanisms outlined in [wire.md](./wire.md).

The envelope is:

```go
// Envelope is an envelope containing all possible messages received through
// the `pairing` wire protocol.
type Envelope struct {
	Disconnect *protocols.Disconnect
	Identity   *Identity
}
```

The protocol is overly simplistic because the underlying stream layer already provides authentication. As only public key exchange is needed in both directions, and nothing else, peers can just announce their data and disconnect afterwards.

```go
// Identity sends the user's `social` protocol P2P identity.
type Identity struct {
	Identity tornet.PublicIdentity // Identity to authenticate with
	Address  tornet.PublicAddress  // Address to contact through
}
```

Notes:

- The `v1` pairing protocol is overly simplistic for prototyping reasons. Future versions need to implement some profile exchange and confirmation too to ensure that you are talking to the correct person **before** trusting them with access to your (public) keys.
