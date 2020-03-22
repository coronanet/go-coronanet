// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package rest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/coronanet/go-coronanet"
)

// profileInfos is the response struct sent back to the client when requesting
// a user profile from the Corona Network.
type profileInfos struct {
	Name string `json:"name"`
}

// serveProfile serves API calls concerning the local user profile.
func (api *api) serveProfile(w http.ResponseWriter, r *http.Request, path string) {
	switch {
	case path == "":
		api.serveProfileInfo(w, r)
	case strings.HasPrefix(path, "/avatar"):
		api.serveProfileAvatar(w, r, strings.TrimPrefix(path, "/avatar"))
	default:
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
	}
}

// serveProfileInfo serves API calls concerning the local user's profile infos.
func (api *api) serveProfileInfo(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "POST":
		// Create a new local user
		switch err := api.backend.CreateProfile(); err {
		case coronanet.ErrProfileExists:
			http.Error(w, "Local user already exists", http.StatusConflict)
		case nil:
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	case "GET":
		// Retrieves the local user's profile
		switch profile, err := api.backend.Profile(); err {
		case coronanet.ErrProfileNotFound:
			http.Error(w, "Local user doesn't exist", http.StatusNotFound)
		case nil:
			w.Header().Add("Content-Type", "application/json")
			json.NewEncoder(w).Encode(&profileInfos{Name: profile.Name})
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	case "PUT":
		// Updates the local user's profile
		profile := new(profileInfos)
		if err := json.NewDecoder(r.Body).Decode(profile); err != nil {
			http.Error(w, "Provided profile is invalid: "+err.Error(), http.StatusBadRequest)
			return
		}
		switch err := api.backend.UpdateProfile(profile.Name); err {
		case coronanet.ErrProfileNotFound:
			http.Error(w, "Local user doesn't exist", http.StatusForbidden)
		case nil:
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	case "DELETE":
		// Deletes the local user (nukes all data)
		if err := api.backend.DeleteProfile(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)

	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

// serveProfileAvatar serves API calls concerning local user's profile picture.
func (api *api) serveProfileAvatar(w http.ResponseWriter, r *http.Request, path string) {
	switch r.Method {
	case "GET":
		// Retrieves the local user's profile and redirect to the immutable URL
		switch profile, err := api.backend.Profile(); {
		case err == coronanet.ErrProfileNotFound:
			http.Error(w, "Local user doesn't exist", http.StatusForbidden)
		case err == nil && profile.Avatar == [32]byte{}:
			http.Error(w, "Local user doesn't have a profile picture", http.StatusNotFound)
		case err == nil:
			http.Redirect(w, r, fmt.Sprintf("/cdn/images/%x", profile.Avatar), http.StatusFound)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	case "PUT":
		// Updates the local user's profile picture

		// Load the entire picture into memory
		r.ParseMultipartForm(1 << 20) // 1MB max profile picture size

		file, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		defer file.Close()

		var buffer bytes.Buffer
		io.Copy(&buffer, file)

		// Attempt to push the image into the database
		switch err := api.backend.UploadProfilePicture(buffer.Bytes()); err {
		case coronanet.ErrProfileNotFound:
			http.Error(w, "Local user doesn't exist", http.StatusForbidden)
		case nil:
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	case "DELETE":
		// Deletes the local user's profile picture
		switch err := api.backend.DeleteProfile(); err {
		case coronanet.ErrProfileNotFound:
			http.Error(w, "Local user doesn't exist", http.StatusForbidden)
		case nil:
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}
