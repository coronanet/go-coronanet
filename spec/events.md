# Corona Network Events

Events in the Corona Network represent real world meetups: house party, office meeting, girls night out, etc. Their purpose is to anonymously link multiple people together, so that they may notify each other about potential infection risks.

Practical details:

- An event is always organized by a single person, who will act as the relay between the participants. This ensures that individual participants don't have to know or trust each other, but can still send and receive alerts.
- An event starts when the organizer creates it and terminates when the organizer closes it. After closure, the organizer of the event will keep maintaining it for `14 days` to allow participants to report infections.
- Although an event can run arbitrarily long, it is recommended to create separate events if something lasts for multiple days, since there's a high probability that the participants are disjoint.
- After the maintenance period expires, the event is frozen and cannot be updated any more. It remains read only for `1 month` to act as a journal, after which all associated information is permanently deleted. There is no way to back it up.
- While the event is running, the organizer can display a QR code, which participant scan to check-in to the event. Checking in requires an active internet connection as the authentication codes are rotated live. This guarantees proof-of-presence.
- Checking in shares absolutely no information with the event organizer, only pings it to bump the attendee count. Information will only ever be sent to the event in case of suspected or positive infection.
- Authorized (checked in) participants can for the duration of the event (+ the `14 day` maintenance period) request infection updates (suspect or confirmed cases at the event) and push their own status too.
- If a participant updates their infection status to confirmed positive, confirmed negative or suspect, that is sent to the event organizer along with their name to permit sanity checking suspicious reports.
- Infection updates from participants are only allowed to transition from `unknown` to `suspect/negative/positive`; and from `suspect` to `negative/positive`. Other transitions are rejected to avoid gaming the system.
- The organizer may manually (e.g. phone call) confirm whether a status update is legitimate. The report and optional checkup will be merged into the event's statistics, but details will not be made available to participants.

![Event Example](images/events_example.png)

## Information cascade

As mentioned above, the main purpose of a single event is to link multiple people together *at a given point in time* (e.g. Alice, Bob and Eve participate in the same barbecue). Participants themselves, however, link multiple events together *across time* (e.g. Alice participated in both the barbecue and a team building). This results in events and people in the system forming a unified graph. This is an immensely powerful construct, as every time a person posts a status update, the cross-people links (through events) and cross-event links (through people) produce an information cascade across the entire network.

![Event Cascade](images/events_cascade.png)

E.g. In the example scenario above, a single `positive` infection update from Alice triggers Bob and Eve to switch over to `suspected` via the `Barbecue` event; who in their own turn cascade the `suspected` status over to Frank via the `Theater` event.

## Technical details

To create an event, an organizer generates a new random cryptographic identity and address, which together will form a `tornet` server. The organizer will run this server for `14 days` after the event ends.

When the organizer wishes to check a participant in, they generate a code consisting of the event's public key, event's public address and a single-use auth token. A participant in will use these credentials for initial contact, through which it negotiates long-term pseudonymous auth credentials.

Participants will periodically `3-6 hours` connect to the event server and retrieve any updated statistics, recalculating their own probability of being infected.

If on the other hand a participant is deemed infected (or suspected) based on participation in other events; or based of self reported test results / symptoms, they will actively attempt to push their status update to the event every `30 minutes`, until they are successful.

![Event Pseudonyms](images/events_pseudonyms.png)

### Technical caveats

**Why use a pseudonym for the event and not the organizer's real identity?**

Events are ephemeral. They last a few weeks, after which they are permanently deleted. By using ephemeral identities, it becomes impossible to track organizers across events.

**Why check in with a pseudonym instead of the participant's real identity?**

Most participants will not send infection updates to events, rather will only gather statistics about their own past presences. By keeping the identity of participants secret, organizers will not be able to track participants across events.

## Event protocol v1 (draft)

The purpose of the `event` protocol is to act as the communication ruleset between an event organizer and the event's participants. The wire protocol follows the general mechanisms outlined in [wire.md](./wire.md).

The envelope is:

```go
// Envelope contains all possible messages sent and received.
type Envelope struct {
	Disconnect  *protocols.Disconnect
	Checkin     *Checkin
	CheckinAck  *CheckinAck
	GetMetadata *GetMetadata
	Metadata    *Metadata
	GetStatus   *GetStatus
	Status      *Status
	Report      *Report
	ReportAck   *ReportAck
}
```

Whenever a connection is made to an event's `tornet` server, the authentication credentials (verified and enforced by `tornet`) defines what phase the connection will be in:

- If the credential is the current ephemeral check-in secret, the remote peer is expected to run a checkin round.
- If the credential is a long-term participant identity from a previous check-in round, the remote peer is expected to exchange data.

*The deadline for finishing the checkin process is `3 seconds`. The idleness timeout for breaking a data exchange connection is `1 minute`.*

### Check-in messages

The checkin process is a fairly straightforward request/reply exchange. The client wishing to check in needs to create its own temporary identity for the event and send it over to the organizer. In addition, the message also needs to contain a digital signature over the original authentication credentials to prove that the client owns the identity.

```go
// Checkin represents a request to attend an event.
type Checkin struct {
	Pseudonym tornet.PublicIdentity // Ephemeral identity to check in with
	Signature tornet.Signature      // Digital signature over the event identity
}
```

Upon receiving a checkin request, the event will verify the signature, and if it checks out will reply with a confirmation. If an error occurs, the connection will be torn down without a reason.

```go
// CheckinAck represents the organizer's response to a checkin request.
type CheckinAck struct{}
```

Independent whether a checkin is successful or not, the authentication credentials is burned and cannot be reused a second time.

### Data exchange messages

After checking in to an event, participants can retrieve some permanent metadata about it. These are social network caliber niceties, mostly meant to have a nicer user experience.

```go
// GetMetadata requests the events permanent metadata.
type GetMetadata struct{}

// Metadata sends the events permanent metadata.
type Metadata struct {
	Name   string // Free form name the event is advertising
	Banner []byte // Binary image of banner, mime not restricted for now
}
```

*Participants should retrieve the metadata once and assume its permanent. This is necessary to avoid organizers from maliciously modifying information or abusing the system for advertising purposes.*

The basic data exchange that participants and the organizer will do is request and return event statistics. The role of these are to warn participants of potential infection risks from the event.

```go
// GetStatus requests the public statistics and infos of an event.
type GetStatus struct {}

// Status contains all the information that's available of the event.
type Status struct {
	Start time.Time // Timestamp when the event started
	End   time.Time // Timestamp when the event ended (0 if not ended)

	Attendees uint // Number of participants in the event
	Negatives uint // Participants who reported negative test results
	Suspected uint // Participants who might have been infected
	Positives uint // Participants who reported positive infection 
}
```

*Participants should check for updates every now and again, but they should not expect real time warnings. A potentially good polling time could be `3-6 hours`.*

If a participant has an infection status update that's relevant for the event's timeline, they can send an update report to the organizer. Beside the new infection status and an optional note, the report also sends over the participant's permanent identity and name to allow out-of-protocol verification of reports. The signature is over the event identity and the report fields (name, status, message). These are used to prevent duplicating reports across events.

The event server will respond, sending back the current infection status associated with the participant. If the report contained an invalid infection status transition, the report is simply ignored and the old status returned. In case of all other errors, the connection is torn down.

```go
// Report is an infection status update from a participant.
type Report struct {
	Name    string // Free form name the user is advertising (might be fake)
	Status  string // Infection status (unknown, negative, suspect, positive)
	Message string // Any personal message for the status update

	Identity  tornet.PublicIdentity // Permanent identity to reporting with
	Signature tornet.Signature      // Signature over the event identity and above fields
}

// ReportAck is a receipt confirmation from the organizer.
type ReportAck struct {
	Status string // Currently maintained infection status
}
```

*If a participant's infection status changes, they should attempt to have it pushed through to all relevant events fast. A potentially good retry time could be `30 minutes`.*
