// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package coronanet

import (
	"encoding/json"

	"github.com/coronanet/go-coronanet/tornet"
)

var (
	// dbContactPrefix is the database key for storing a remote user's profile.
	dbContactPrefix = []byte("contact-")
)

// contact represents a remote user's profile information.
type contact struct {
	Name   string   `json:"name`    // Originally remote, can override
	Avatar [32]byte `json:"avatar"` // Always remote, for now
}

// AddContact inserts a new remote identity into the local trust ring and adds
// it to the overlay network.
func (b *Backend) AddContact(id *tornet.PublicIdentity) (string, error) {
	b.lock.Lock()
	defer b.lock.Unlock()

	// Inject the security credentials into the local profile and create the profile
	// entry for the remote user.
	uid := id.ID()
	if err := b.addContact(id); err != nil {
		return "", err
	}
	blob, err := json.Marshal(&contact{})
	if err != nil {
		return "", err
	}
	if err := b.database.Put(append(dbContactPrefix, uid...), blob, nil); err != nil {
		return "", err
	}
	// If the overlay network is currently online, enable networking with this user
	if b.overlay == nil {
		return uid, nil
	}
	return uid, b.overlay.Trust(uid, id)
}

// Contacts returns the unique ids of all the current contacts.
func (b *Backend) Contacts() ([]string, error) {
	b.lock.RLock()
	defer b.lock.RUnlock()

	prof, err := b.Profile()
	if err != nil {
		return nil, ErrProfileNotFound
	}
	ids := make([]string, 0, len(prof.Ring))
	for id := range prof.Ring {
		ids = append(ids, id)
	}
	return ids, nil
}

// Contact retrieves a remote user's profile infos.
func (b *Backend) Contact(id string) (*contact, error) {
	blob, err := b.database.Get(append(dbContactPrefix, id...), nil)
	if err != nil {
		return nil, ErrContactNotFound
	}
	info := new(contact)
	if err := json.Unmarshal(blob, info); err != nil {
		return nil, err
	}
	return info, nil
}

// UpdateContact overrides the profile information of an existing remote user.
func (b *Backend) UpdateContact(id string, name string) error {
	// Retrieve the current profile and abort if the update is a noop
	info, err := b.Contact(id)
	if err != nil {
		return err
	}
	if info.Name == name {
		return nil
	}
	// Name changed, update and serialize back to disk
	info.Name = name

	blob, err := json.Marshal(info)
	if err != nil {
		return err
	}
	return b.database.Put(append(dbContactPrefix, id...), blob, nil)
}

// uploadContactPicture uploads a new local profile picture for the remote user.
func (b *Backend) uploadContactPicture(id string, data []byte) error {
	// Retrieve the current profile to ensure the user exists
	info, err := b.Contact(id)
	if err != nil {
		return err
	}
	// Upload the image into the CDN and delete the old one
	hash, err := b.uploadCDNImage(data)
	if err != nil {
		return err
	}
	if info.Avatar != ([32]byte{}) {
		if err := b.deleteCDNImage(info.Avatar); err != nil {
			return err
		}
	}
	// If the hash changed, update the profile
	if info.Avatar == hash {
		return nil
	}
	info.Avatar = hash

	blob, err := json.Marshal(info)
	if err != nil {
		return err
	}
	return b.database.Put(append(dbContactPrefix, id...), blob, nil)
}

// deleteContactPicture deletes the existing local profile picture of the remote user.
func (b *Backend) deleteContactPicture(id string) error {
	// Retrieve the current profile to ensure the user exists
	info, err := b.Contact(id)
	if err != nil {
		return err
	}
	if info.Avatar == [32]byte{} {
		return nil
	}
	// Profile picture exists, delete it from the CDN and update the profile
	if err := b.deleteCDNImage(info.Avatar); err != nil {
		return err
	}
	info.Avatar = [32]byte{}

	blob, err := json.Marshal(info)
	if err != nil {
		return err
	}
	return b.database.Put(append(dbContactPrefix, id...), blob, nil)
}
