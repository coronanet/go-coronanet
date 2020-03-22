// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package coronanet

import (
	"encoding/json"
	"errors"

	"github.com/coronanet/go-coronanet/tornet"
	"github.com/syndtr/goleveldb/leveldb/util"
)

var (
	dbProfileKey = []byte("profile")

	// ErrProfileNotFound is returned if the profile is attempted to be read from
	// the database but it does not exist.
	ErrProfileNotFound = errors.New("profile not found")

	// ErrProfileExists is returned if a new profile is attempted to be created
	// but an old one already exists.
	ErrProfileExists = errors.New("profile already exists")
)

// profile represents a local user's profile information, both public and private.
type profile struct {
	Key    *tornet.SecretIdentity `json:"key"`
	Name   string                 `json:"name`
	Avatar [32]byte               `json:"avatar"`
}

// CreateProfile generates a new cryptographic identity for the local used and
// injects it into the system.
func (b *Backend) CreateProfile() error {
	// Make sure there's no already existing user
	if _, err := b.Profile(); err == nil {
		return ErrProfileExists
	}
	// Generate a new profile key and upload it
	key, err := tornet.GenerateIdentity()
	if err != nil {
		return err
	}
	blob, err := json.Marshal(&profile{Key: key})
	if err != nil {
		return err
	}
	return b.database.Put(dbProfileKey, blob, nil)
}

// DeleteProfile wipes the entire database of everything. It's unforgiving, no
// backups, no restore, the data is gone!
func (b *Backend) DeleteProfile() error {
	// Retrieve the current profile and abort if it doesn't exist
	if _, err := b.Profile(); err != nil {
		return err
	}
	// Profile existed, nuke the database
	it := b.database.NewIterator(&util.Range{nil, nil}, nil)
	for it.Next() {
		b.database.Delete(it.Key(), nil)
	}
	it.Release()

	return b.database.CompactRange(util.Range{nil, nil})
}

// Profile retrieves the current user's profile infos.
func (b *Backend) Profile() (*profile, error) {
	blob, err := b.database.Get(dbProfileKey, nil)
	if err != nil {
		return nil, ErrProfileNotFound
	}
	prof := new(profile)
	if err := json.Unmarshal(blob, prof); err != nil {
		return nil, err
	}
	return prof, nil
}

// UpdateProfile changes the profile information of an existing local user.
func (b *Backend) UpdateProfile(name string) error {
	// Retrieve the current profile and abort if the update is a noop
	prof, err := b.Profile()
	if err != nil {
		return err
	}
	if prof.Name == name {
		return nil
	}
	// Name changed, update and serialize back to disk
	prof.Name = name

	blob, err := json.Marshal(prof)
	if err != nil {
		return err
	}
	return b.database.Put(dbProfileKey, blob, nil)
}

// UploadProfilePicture uploads a new profile picture for the user.
func (b *Backend) UploadProfilePicture(data []byte) error {
	// Retrieve the current profile to ensure the user exists
	prof, err := b.Profile()
	if err != nil {
		return err
	}
	// Upload the image into the CDN and delete the old one
	hash, err := b.uploadCDNImage(data)
	if err != nil {
		return err
	}
	if prof.Avatar != ([32]byte{}) {
		if err := b.deleteCDNImage(prof.Avatar); err != nil {
			return err
		}
	}
	// If the hash changed, update the profile
	if prof.Avatar == hash {
		return nil
	}
	prof.Avatar = hash

	blob, err := json.Marshal(prof)
	if err != nil {
		return err
	}
	return b.database.Put(dbProfileKey, blob, nil)
}

// DeleteProfilePicture deletes the existing profile picture of the user.
func (b *Backend) DeleteProfilePicture() error {
	// Retrieve the current profile to ensure the user exists
	prof, err := b.Profile()
	if err != nil {
		return err
	}
	if prof.Avatar == [32]byte{} {
		return nil
	}
	// Profile picture exists, delete it from the CDN and update the profile
	if err := b.deleteCDNImage(prof.Avatar); err != nil {
		return err
	}
	prof.Avatar = [32]byte{}

	blob, err := json.Marshal(prof)
	if err != nil {
		return err
	}
	return b.database.Put(dbProfileKey, blob, nil)
}
