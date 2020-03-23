// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package coronanet

import "github.com/coronanet/go-coronanet/tornet"

// AddContact inserts a new remote identity into the local trust ring and adds
// it to the overlay network.
func (b *Backend) AddContact(id *tornet.PublicIdentity) error {
	b.lock.RLock()
	defer b.lock.RUnlock()

	if err := b.addContact(id); err != nil {
		return err
	}
	if b.overlay == nil {
		return nil
	}
	return b.overlay.Trust(id.ID(), id)
}
