// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package coronanet

import (
	"encoding/json"
	"errors"

	"github.com/coronanet/go-coronanet/tornet"
)

var (
	// dbContactPrefix is the database key for storing a remote user's profile.
	dbContactPrefix = []byte("contact-")

	// ErrSelfContact is returned if a new contact is attempted to be trusted
	// but it is the local user.
	ErrSelfContact = errors.New("cannot contact self")

	// ErrContactNotFound is returned if a new contact is attempted to be accessed
	// but it does not exist.
	ErrContactNotFound = errors.New("contact not found")

	// ErrContactExists is returned if a new contact is attempted to be trusted
	// but it already is trusted.
	ErrContactExists = errors.New("contact already exists")
)

// contact represents a remote user's profile information.
type contact struct {
	Name   string   `json:"name`    // Originally remote, can override
	Avatar [32]byte `json:"avatar"` // Always remote, for now
}

// AddContact inserts a new remote identity into the local trust ring and adds
// it to the overlay network.
func (b *Backend) AddContact(keyring tornet.RemoteKeyRing) (tornet.IdentityFingerprint, error) {
	b.logger.Info("Creating new contact", "contact", keyring.Identity.Fingerprint())

	b.lock.Lock()
	defer b.lock.Unlock()

	// Sanity check that the contact does not exist
	prof, err := b.Profile()
	if err != nil {
		return "", err
	}
	uid := keyring.Identity.Fingerprint()
	if prof.KeyRing.Identity.Fingerprint() == uid {
		return "", ErrSelfContact
	}
	if _, err := b.Contact(uid); err == nil {
		return "", ErrContactExists
	}
	// Create the profile entry for the remote user
	blob, err := json.Marshal(&contact{})
	if err != nil {
		return "", err
	}
	if err := b.database.Put(append(dbContactPrefix, uid...), blob, nil); err != nil {
		return "", err
	}
	// Inject the security credentials into the overlay (cascading into the profile)
	return uid, b.overlay.Trust(keyring)
}

// DeleteContact removes the contact from the trust ring, deletes all associated
// data and disconnects any active connections.
func (b *Backend) DeleteContact(uid tornet.IdentityFingerprint) error {
	b.logger.Info("Deleting contact", "contact", uid)

	b.lock.Lock()
	defer b.lock.Unlock()

	// Sanity check that the contact does exist
	if _, err := b.Contact(uid); err != nil {
		return ErrContactNotFound
	}
	// Break any pending connections from the overlay network
	if err := b.overlay.Untrust(uid); err != nil {
		return err
	}
	// Remove all data associated with the contact
	if err := b.deleteContactPicture(uid); err != nil {
		return err
	}
	return b.database.Delete(append(dbContactPrefix, uid...), nil)
}

// Contacts returns the unique ids of all the current contacts.
func (b *Backend) Contacts() ([]tornet.IdentityFingerprint, error) {
	prof, err := b.Profile()
	if err != nil {
		return nil, ErrProfileNotFound
	}
	uids := make([]tornet.IdentityFingerprint, 0, len(prof.KeyRing.Trusted))
	for uid := range prof.KeyRing.Trusted {
		uids = append(uids, uid)
	}
	return uids, nil
}

// Contact retrieves a remote user's profile infos.
func (b *Backend) Contact(uid tornet.IdentityFingerprint) (*contact, error) {
	blob, err := b.database.Get(append(dbContactPrefix, uid...), nil)
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
func (b *Backend) UpdateContact(uid tornet.IdentityFingerprint, name string) error {
	b.logger.Info("Updating contact infos", "contact", uid, "name", name)

	b.lock.Lock()
	defer b.lock.Unlock()

	// Retrieve the current profile and abort if the update is a noop
	info, err := b.Contact(uid)
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
	return b.database.Put(append(dbContactPrefix, uid...), blob, nil)
}

// uploadContactPicture uploads a new local profile picture for the remote user.
func (b *Backend) uploadContactPicture(uid tornet.IdentityFingerprint, data []byte) error {
	b.logger.Info("Uploading contact picture", "contact", uid)

	b.lock.Lock()
	defer b.lock.Unlock()

	// Retrieve the current profile to ensure the user exists
	info, err := b.Contact(uid)
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
	return b.database.Put(append(dbContactPrefix, uid...), blob, nil)
}

// deleteContactPicture deletes the existing local profile picture of the remote user.
func (b *Backend) deleteContactPicture(uid tornet.IdentityFingerprint) error {
	b.logger.Info("Deleting contact picture", "contact", uid)

	b.lock.Lock()
	defer b.lock.Unlock()

	// Retrieve the current profile to ensure the user exists
	info, err := b.Contact(uid)
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
	return b.database.Put(append(dbContactPrefix, uid...), blob, nil)
}
