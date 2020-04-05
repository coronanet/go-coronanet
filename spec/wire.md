# Corona Network Protocol

This document specifies the wire protocol format for the Corona Network's P2P infrastructure.

The wire format is a Gob message protocol ([`encoding/gob`](https://golang.org/pkg/encoding/gob/)) on top of a stream protocol. We assume that peer identification, peer authorization and data encryption are already solved at the stream level. A Gob message stream has some intriguing properties:

- The stream is self describing: all type information is passed along the data, but opposed to usual protocols that describe each and every message, Gob will only ever transmit type information once, at it's first occurrence.
- Arbitrary Go types are supported: apart from certain limitations around `nil` pointers and circular references, almost anything can be encoded and decoded, without having to define the algorithm to do so.
- Decoding is very forgiving: as the stream is self describing, message fields can be shuffled around; new ones added and old ones deleted. This permits many protocol changes to be done without duplicating message formats.

*As a general note, whilst many of the messages in the protocol mimic a request / response pattern, this is not mandatory. Network participants are free to send arbitrary updated they seem useful without being asked first.*

## Enveloping

Since a `gob` stream needs to know in advance what data type to unmarshal into, we can't use the usual protocol message parsing logic of checking a message code and then switching on it. Instead, we create a master packet (envelope) that contains an instance of every possible message within it. Gob will be smart and not transmit `nil` pointers, so this approach is not wasteful.

```go
// CoronaMessage contains all possible messages received.
type CoronaMessage struct {
	Handshake  *system.Handshake
	Disconnect *system.Disconnect
	GetProfile *corona.GetProfile
	Profile    *corona.Profile
	GetAvatar  *corona.GetAvatar
	Avatar     *corona.Avatar
}
```

The above envelope is for the Corona protocol. A similar one is used for the Pairing protocol, just with the `corona` messages swapped out for the `pairing` ones.

## System Protocol

The first message exchanged is the protocol handshake, which describes the capabilities of the peers to each other. The role of the handshake is to specify which protocol and which versions each can speak and choose the highest common one.

The reason for including a protocol name too into the handshake is to allow certain connections to be used for completely different purposes than others. The current protocols are:

- `corona`:  Main protocol used to maintain the social network 
- `pairing`: Side protocol used to pair untrusted users together


```go
// Handshake represents the initial protocol version negotiation.
type Handshake struct {
	Protocol string // Protocol expected on this connection
	Versions []uint // Protocol version numbers supported
}
```

At any point in time during message exchange, either side can request the connection to be torn down. There is no pre-defined list of reasons that peers might give each other, rather only a free-form reason for developers:
 
 - Programs can rarely meaningfully act on a disconnect reason. Generally they will just use their local knowledge to decide to reconnect or not, independent of what the remote side says.
 - A predefined list of reasons always ends up being insufficient to describe all faults accurately. As such, developers end up reusing the same disconnect reason for a variety of cases, losing their meaning altogether.

A disconnect code for programmatic interpretation can always be added later if there's a strong enough technical necessity for it.

```go
// Disconnect represents a notification that the connection is torn down.
type Disconnect struct {
	Reason string // Textual disconnect reason, meant for developers
}
```

Opposed to many peer-to-peer protocols, the Corona Network wire protocol is passive. There is no active chatter going on non-stop and there are no heartbeat messages. Instead, nodes only rarely connect to each other, when they have something to share, exchange their data and disconnect.

## Corona Protocol v1 (draft)

### User profile messages

The Corona Network features a mini social network with a very limited capability for users to announce and retrieve information about each other. The amount of profile information is deliberately super small to keep sensitive data out of the system; it's purpose is only to make certain UI tasks simpler.

Users can request the remote side's profile information and will receive some basic infos and a summary of extended fields that would require costly retrievals.

```go
// GetProfile requests the remote user's profile summary.
type GetProfile struct {}

// Profile sends the current user's profile summary.
type Profile struct {
	Name   string   // Free form name the user is advertising (might be fake)
	Avatar [32]byte // SHA3 hash of the user's avatar (avoid download if known)
}
```

*Although clients will always return the name too in their response, this field may (read, generally will) be ignored to avoid faking someone else.*

---

As seen above, the user's profile picture is not sent back in the response, to avoid downloading a large chunk of data only to realise it hasn't changed. Instead, it's SHA3 hash is returned, based on which the caller can decide to request or not. The profile picture retrieval is:

```go
// GetAvatar requests the remote user's profile picture.
type GetAvatar struct {}

// Avatar sends the current user's profile picture.
type Avatar struct {
	Image []byte // Binary image content, mime not restricted for now
}
```

*It is the callers sole discretion when it requests the profile / avatar from a remote connection. It might request it only after pairing and never again; it might do it once per connection; or maybe even periodically.*

## Pairing Protocol v1 (draft)

The pairing protocol is a support mechanism disjoint from the main peer-to-peer protocol. It's purpose is to be an out-of-bounds secret-exchange between two nodes so they can share their identities and join each other on the main protocol.

The protocol is overly simplistic because the underlying stream layer already provides authentication. As only public key exchange is needed in both directions, and nothing else, peers can just announce their data and disconnect afterwards.

```go
// Identity sends the user's Corona protocol P2P identity.
type Identity struct {
	Blob []byte // Encoded tornet public identity, internal format
}
```

Notes:

- The `v1` pairing protocol is overly simplistic for prototyping reasons. Future versions need to implement some profile exchange and confirmation too to ensure that you are talking to the correct person **before** trusting them with access to your (public) keys.