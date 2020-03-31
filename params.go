// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package coronanet

import "time"

const (
	// connectionIdleTimeout is the maximum amount of time for a connection to
	// remain idle before it is torn down (to save bandwidth and battery).
	connectionIdleTimeout = 5 * time.Minute

	// schedulerSanityRedial is the time to wait before redialing a peer if no
	// event happens in between.
	schedulerSanityRedial = 24 * time.Hour

	// schedulerFailureRedial is the time to wait before redialing a peer which
	// was unreachable the last time we dialed.
	schedulerFailureRedial = time.Hour

	// schedulerProfileUpdate is the time to wait before dialing someone to push
	// over a profile update.
	schedulerProfileUpdate = 6 * time.Hour
)
