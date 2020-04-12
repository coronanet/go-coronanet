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
	"github.com/ethereum/go-ethereum/log"
)

// ProfileInfos is the response struct sent back to the client when requesting
// a user profile from the Corona Network.
type ProfileInfos struct {
	Name string `json:"name"`
}

// serveProfile serves API calls concerning the local user profile.
func (api *api) serveProfile(w http.ResponseWriter, r *http.Request, path string, logger log.Logger) {
	switch {
	case path == "":
		api.serveProfileInfo(w, r, logger)
	case strings.HasPrefix(path, "/avatar"):
		api.serveProfileAvatar(w, r, logger)
	default:
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
	}
}

// serveProfileInfo serves API calls concerning the local user's profile infos.
func (api *api) serveProfileInfo(w http.ResponseWriter, r *http.Request, logger log.Logger) {
	switch r.Method {
	case "POST":
		// Create a new local user
		logger.Debug("Requesting profile creation")
		switch err := api.backend.CreateProfile(); err {
		case coronanet.ErrProfileExists:
			logger.Warn("Local user already exists")
			http.Error(w, "Local user already exists", http.StatusConflict)
		case nil:
			logger.Debug("Profile successfully created")
			w.WriteHeader(http.StatusOK)
		default:
			logger.Error("Profile creation failed", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	case "GET":
		// Retrieves the local user's profile
		logger.Debug("Requesting profile information")
		switch profile, err := api.backend.Profile(); err {
		case coronanet.ErrProfileNotFound:
			logger.Warn("Local user doesn't exist")
			http.Error(w, "Local user doesn't exist", http.StatusNotFound)
		case nil:
			logger.Debug("Profile successfully retrieved", "name", profile.Name)
			w.Header().Add("Content-Type", "application/json")
			json.NewEncoder(w).Encode(&ProfileInfos{Name: profile.Name})
		default:
			logger.Error("Profile retrieval failed", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	case "PUT":
		// Updates the local user's profile
		logger.Debug("Requesting profile update")
		profile := new(ProfileInfos)
		if err := json.NewDecoder(r.Body).Decode(profile); err != nil {
			logger.Error("Provided profile is invalid", "err", err)
			http.Error(w, "Provided profile is invalid: "+err.Error(), http.StatusBadRequest)
			return
		}
		switch err := api.backend.UpdateProfile(profile.Name); err {
		case coronanet.ErrProfileNotFound:
			logger.Warn("Local user doesn't exist")
			http.Error(w, "Local user doesn't exist", http.StatusForbidden)
		case nil:
			logger.Debug("Profile successfully updated")
			w.WriteHeader(http.StatusOK)
		default:
			logger.Error("Profile updating failed", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	case "DELETE":
		// Deletes the local user (nukes all data)
		logger.Debug("Requesting profile deletion")
		if err := api.backend.DeleteProfile(); err != nil {
			logger.Error("Profile deletion failed", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		logger.Debug("Profile successfully deleted")
		w.WriteHeader(http.StatusOK)

	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

// serveProfileAvatar serves API calls concerning local user's profile picture.
func (api *api) serveProfileAvatar(w http.ResponseWriter, r *http.Request, logger log.Logger) {
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
