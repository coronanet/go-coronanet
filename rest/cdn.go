// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package rest

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"

	"github.com/coronanet/go-coronanet"
)

// serveCDN serves API calls concerning immutable content distribution.
func (api *api) serveCDN(w http.ResponseWriter, r *http.Request, path string) {
	switch {
	case strings.HasPrefix(path, "/images"):
		api.serveCDNImages(w, r, strings.TrimPrefix(path, "/images"))
	default:
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
	}
}

// serveCDNImages serves API calls concerning immutable image distribution.
func (api *api) serveCDNImages(w http.ResponseWriter, r *http.Request, path string) {
	// If the image sha3 is of wrong length, reject the request
	if len(path) != 65 {
		http.Error(w, "Image hash invalid", http.StatusBadRequest)
		return
	}
	var hash [32]byte
	if _, err := hex.Decode(hash[:], []byte(path[1:])); err != nil {
		http.Error(w, fmt.Sprintf("Image hash invalid: %s", err), http.StatusBadRequest)
		return
	}
	// Hash valid, try to return it to the user
	switch r.Method {
	case "GET":
		// Retrieves the local user's profile
		switch data, err := api.backend.CDNImage(hash); err {
		case coronanet.ErrImageNotFound:
			http.Error(w, "Image unknown or unavailable", http.StatusNotFound)
		case nil:
			w.Header().Add("Content-Type", "image/jpeg")
			w.Write(data)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}
