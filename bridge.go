// coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package coronanet

import "github.com/ipsn/go-ghostbridge"

// Bridge is a tiny struct (re)definition so gomobile will export all the built
// in methods of the underlying ghostbridge.Bridge struct.
type Bridge struct {
	*ghostbridge.Bridge
}

// NewBridge creates an instance of the ghost bridge, typed such as gomobile to
// generate a Bridge constructor out of it.
func NewBridge() (*Bridge, error) {
	bridge, err := ghostbridge.New(new(backend))
	if err != nil {
		return nil, err
	}
	return &Bridge{bridge}, nil
}
