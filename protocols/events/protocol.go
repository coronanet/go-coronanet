// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

// Package events implements the `events` protocol.
package events

import (
	"time"

	"github.com/coronanet/go-coronanet/protocols"
	"github.com/coronanet/go-coronanet/tornet"
)

// Protocol is the unique identifier of the events protocol.
const Protocol = "events"

// Envelope is an envelope containing all possible messages received through
// the `events` wire protocol.
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

// Checkin represents a request to attend an event.
type Checkin struct {
	Pseudonym tornet.PublicIdentity // Ephemeral identity to check in with
	Signature tornet.Signature      // Digital signature over the event identity
}

// CheckinAck represents the organizer's response to a checkin request.
type CheckinAck struct{}

// GetMetadata requests the events permanent metadata.
type GetMetadata struct{}

// Metadata sends the events permanent metadata.
type Metadata struct {
	Name   string // Free form name the event is advertising
	Banner []byte // Binary image of banner, mime not restricted for now
}

// GetStatus requests the public statistics and infos of an event.
type GetStatus struct{}

// Status contains all the information that's available of the event.
type Status struct {
	Start time.Time // Timestamp when the event started
	End   time.Time // Timestamp when the event ended (0 if not ended)

	Attendees uint // Number of participants in the event
	Negatives uint // Participants who reported negative test results
	Suspected uint // Participants who might have been infected
	Positives uint // Participants who reported positive infection
}

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
