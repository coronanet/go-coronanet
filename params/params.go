// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

// Package params contains constants relevant to all subsystems.
package params

import "time"

const (
	// InfectionStatusUnknown is the constant representing unknown infection
	// status, meaning that too much time passed without any specific report
	// to be meaningfully probably.
	InfectionStatusUnknown = "unknown"

	// InfectionStatusNegative is the constant representing a successful test
	// resulting in negative outcome.
	InfectionStatusNegative = "negative"

	// InfectionStatusSuspected is the constant representing suspicion of infection,
	// either self-reported by the user or auto-derived by the system.
	InfectionStatusSuspected = "suspected"

	// InfectionStatusPositive is the constant representing a successful test
	// resulting in positive outcome.
	InfectionStatusPositive = "positive"
)

const (
	// EventInfectionUpdateRetry is the time period to try reconnection after if
	// the user wants to push an infection status update out.
	EventInfectionUpdateRetry = 30 * time.Minute

	// EventStatsRecheck is the time period after which to reconnect to an event
	// to check for status updates.
	EventStatsRecheck = 6 * time.Hour

	// EventMaintenancePeriod is the time an event will be kept alive after its
	// end time. Networking will be disabled after this passes but the organizer
	// and attendees will still be able to access it.
	EventMaintenancePeriod = 14 * 24 * time.Hour

	// EventArchivePeriod is the time an event will be kept archived after its
	// maintenance period expires. After this time expires, all data associated
	// with the event is deleted.
	EventArchivePeriod = 30 * 24 * time.Hour
)
