// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package coronanet

import (
	"encoding/binary"
	"errors"

	"golang.org/x/crypto/sha3"
)

var (
	dbCDNImagePrefix    = []byte("cdn-image-")
	dbCDNImageRefSuffix = []byte("-refs")

	// ErrImageNotFound is returned if an image is attempted to be read from the
	// CDN but it is not found.
	ErrImageNotFound = errors.New("image not found")
)

// uploadCDNImage inserts a binary image blob by hash into the CND and increments
// its reference count.
func (b *Backend) uploadCDNImage(data []byte) ([32]byte, error) {
	// Calculate the image hash to use as a database key
	hash := sha3.Sum256(data)

	// Retrieve the number of live references to this hash
	var refs uint64
	if blob, err := b.database.Get(append(append(dbCDNImagePrefix, hash[:]...), dbCDNImageRefSuffix...), nil); err == nil {
		refs, _ = binary.Uvarint(blob) // TODO(karalabe): Maybe check for errors?
	}
	// If there are no live references, upload the image; either way, bump the refs
	if refs == 0 {
		if err := b.database.Put(append(dbCDNImagePrefix, hash[:]...), data, nil); err != nil {
			return [32]byte{}, err
		}
	}
	blob := make([]byte, binary.MaxVarintLen64)
	blob = blob[:binary.PutUvarint(blob, refs+1)]
	return hash, b.database.Put(append(append(dbCDNImagePrefix, hash[:]...), dbCDNImageRefSuffix...), blob, nil)
}

// deleteCDNImage dereferences an image from the CDN and deletes it if the ref
// count reaches zero.
func (b *Backend) deleteCDNImage(hash [32]byte) error {
	// Retrieve the number of live references to this hash, skip if zero
	var refs uint64
	if blob, err := b.database.Get(append(append(dbCDNImagePrefix, hash[:]...), dbCDNImageRefSuffix...), nil); err == nil {
		refs, _ = binary.Uvarint(blob) // TODO(karalabe): Maybe check for errors?
	}
	if refs == 0 {
		return nil
	}
	// If there is only one reference, delete the image; either way, drop the refs
	if refs == 1 {
		if err := b.database.Delete(append(dbCDNImagePrefix, hash[:]...), nil); err != nil {
			return err
		}
	}
	blob := make([]byte, binary.MaxVarintLen64)
	blob = blob[:binary.PutUvarint(blob, refs-1)]
	return b.database.Put(append(append(dbCDNImagePrefix, hash[:]...), dbCDNImageRefSuffix...), blob, nil)
}

// CDNImage retrieves an image from the CDN.
func (b *Backend) CDNImage(hash [32]byte) ([]byte, error) {
	blob, err := b.database.Get(append(dbCDNImagePrefix, hash[:]...), nil)
	if err != nil {
		return nil, ErrImageNotFound
	}
	return blob, nil
}
