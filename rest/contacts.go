// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package rest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/coronanet/go-coronanet"
)

// serveContacts serves API calls concerning all contacts.
func (api *api) serveContacts(w http.ResponseWriter, r *http.Request, path string) {
	// If we're not serving the contacts root, descend into a single contact
	if path != "" {
		api.serveContact(w, r, path)
		return
	}
	// Handle serving the contacts root
	switch r.Method {
	case "GET":
		// List all contacts of the local user
		switch contacts, err := api.backend.Contacts(); err {
		case coronanet.ErrProfileNotFound:
			http.Error(w, "Local user doesn't exist", http.StatusForbidden)
		case nil:
			w.Header().Add("Content-Type", "application/json")
			json.NewEncoder(w).Encode(contacts)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

// serveContact serves API calls concerning a single remote contact.
func (api *api) serveContact(w http.ResponseWriter, r *http.Request, path string) {
	// All contact APIs need to provide the unique id
	if len(path) < 65 || (len(path) > 65 && path[65] != '/') {
		http.Error(w, "Contact ID invalid", http.StatusBadRequest)
		return
	}
	id := path[1:65]
	path = path[65:]

	// If we're not serving the contact root, descend into the profile
	if path != "" {
		api.serveContactProfile(w, r, id, path)
		return
	}
	// Handle serving the contact root
	switch r.Method {
	case "DELETE":
		// Removes an existing contact
		switch err := api.backend.DeleteContact(id); err {
		case coronanet.ErrContactNotFound:
			http.Error(w, "Remote contact doesn't exist", http.StatusForbidden)
		case nil:
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

// serveContactProfile serves API calls concerning a remote contact profile.
func (api *api) serveContactProfile(w http.ResponseWriter, r *http.Request, id string, path string) {
	switch {
	case path == "/profile":
		api.serveContactProfileInfo(w, r, id)
	case strings.HasPrefix(path, "/profile/avatar"):
		api.serveContactProfileAvatar(w, r, id)
	default:
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
	}
}

// serveContactProfileInfo serves API calls concerning the local user's profile infos.
func (api *api) serveContactProfileInfo(w http.ResponseWriter, r *http.Request, id string) {
	switch r.Method {
	case "GET":
		// Retrieves a remote contact's profile
		switch contact, err := api.backend.Contact(id); err {
		case coronanet.ErrContactNotFound:
			http.Error(w, "Remote contact doesn't exist", http.StatusNotFound)
		case nil:
			w.Header().Add("Content-Type", "application/json")
			json.NewEncoder(w).Encode(&profileInfos{Name: contact.Name})
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	case "PUT":
		// Overrides the remote contact's profile
		profile := new(profileInfos)
		if err := json.NewDecoder(r.Body).Decode(profile); err != nil {
			http.Error(w, "Provided profile is invalid: "+err.Error(), http.StatusBadRequest)
			return
		}
		switch err := api.backend.UpdateContact(id, profile.Name); err {
		case coronanet.ErrContactNotFound:
			http.Error(w, "Remote contact doesn't exist", http.StatusForbidden)
		case nil:
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

// serveContactProfileAvatar serves API calls concerning a remote user's profile picture.
func (api *api) serveContactProfileAvatar(w http.ResponseWriter, r *http.Request, id string) {
	switch r.Method {
	case "GET":
		// Retrieves the remote contact's profile and redirect to the immutable URL
		switch contact, err := api.backend.Contact(id); {
		case err == coronanet.ErrContactNotFound:
			http.Error(w, "Remote contact doesn't exist", http.StatusForbidden)
		case err == nil && contact.Avatar == [32]byte{}:
			http.Error(w, "Remote contact doesn't have a profile picture", http.StatusNotFound)
		case err == nil:
			http.Redirect(w, r, fmt.Sprintf("/cdn/images/%x", contact.Avatar), http.StatusFound)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}
