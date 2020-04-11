// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package events

import (
	"time"

	"github.com/coronanet/go-coronanet/params"
)

const (
	// connectionIdleTimeout is the maximum amount of time for a connection to
	// remain idle before it is torn down (to save bandwidth and battery).
	connectionIdleTimeout = time.Minute

	// checkinTimeout is the maximum amount of time for a checkin to complete
	// before the connection is torn down.
	checkinTimeout = 3 * time.Second
)

// validInfectionStatus returns if the `status` string is valid according to the
// `events` protocol.
func validInfectionStatus(status string) bool {
	return status == params.InfectionStatusUnknown || status == params.InfectionStatusNegative ||
		status == params.InfectionStatusSuspected || status == params.InfectionStatusPositive
}

// validInfectionTransition returns whether the `events` protocol permits going
// form the `old` infection status to the `new` one. The purpose of the enforced
// limitation is ensure the system reached a stable point eventually/
func validInfectionTransition(old string, new string) bool {
	// If nothing changed, reject the transition (avoids data mining)
	if old == new {
		return false
	}
	// If the new status is unknown, reject the transition (avoids data mining)
	if new == "" || new == params.InfectionStatusUnknown {
		return false
	}
	// If the status is already confirmed positive or negative, there's nowhere to go
	if old == params.InfectionStatusNegative || old == params.InfectionStatusPositive {
		return false
	}
	// At this point `old` is either `unknown` or `suspect` and `new` is higher, accept
	return true
}
