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
	"github.com/coronanet/go-coronanet/protocols/events"
	"github.com/coronanet/go-coronanet/tornet"
)

// eventConfig is the initial configurations of an event when creating it.
type eventConfig struct {
	Name string `json:"name"`
}

// serveEvents serves API calls concerning all events.
func (api *api) serveEvents(w http.ResponseWriter, r *http.Request, path string) {
	switch {
	case strings.HasPrefix(path, "/hosted"):
		api.serveHostedEvents(w, r, strings.TrimPrefix(path, "/hosted"))
	case strings.HasPrefix(path, "/joined"):
		api.serveJoinedEvents(w, r, strings.TrimPrefix(path, "/joined"))
	default:
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
	}
}

// serveHostedEvents serves API calls concerning hosted events.
func (api *api) serveHostedEvents(w http.ResponseWriter, r *http.Request, path string) {
	// If we're not serving the events root, descend into a single event
	if path != "" {
		api.serveHostedEvent(w, r, path)
		return
	}
	// Handle serving the events root
	switch r.Method {
	case "GET":
		// List all the hosted events
		w.Header().Add("Content-Type", "application/json")
		json.NewEncoder(w).Encode(api.backend.HostedEvents())

	case "POST":
		// Hosts a new event
		config := new(eventConfig)
		if err := json.NewDecoder(r.Body).Decode(config); err != nil {
			http.Error(w, "Provided event config is invalid: "+err.Error(), http.StatusBadRequest)
			return
		}
		switch uid, err := api.backend.CreateEvent(config.Name); err {
		case coronanet.ErrProfileNotFound:
			http.Error(w, "Local user doesn't exist", http.StatusForbidden)
		case nil:
			w.Header().Add("Content-Type", "application/json")
			json.NewEncoder(w).Encode(uid)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

// serveHostedEvent serves API calls concerning a single hosted events.
func (api *api) serveHostedEvent(w http.ResponseWriter, r *http.Request, path string) {
	// All event APIs need to provide the unique id
	parts := strings.SplitN(path[1:], "/", 2)

	uid := tornet.IdentityFingerprint(parts[0])
	path = ""
	if len(parts) > 1 {
		path = "/" + parts[1]
	}
	// If we're not serving the event root, descend further down
	if path != "" {
		switch {
		case strings.HasPrefix(path, "/banner"):
			api.serveHostedEventBanner(w, r, uid)
		case strings.HasPrefix(path, "/checkin"):
			api.serveHostedEventCheckin(w, r, uid)
		default:
			http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		}
		return
	}
	// Handle serving the event root
	switch r.Method {
	case "GET":
		// Retrieves a hosted event's statistics
		switch infos, err := api.backend.HostedEvent(uid); err {
		case coronanet.ErrEventNotFound:
			http.Error(w, "Hosted event doesn't exist", http.StatusNotFound)
		case nil:
			w.Header().Add("Content-Type", "application/json")
			json.NewEncoder(w).Encode(infos.Stats())
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	case "DELETE":
		// Terminates the event, will be cleaned up automatically
		switch err := api.backend.TerminateEvent(uid); err {
		case coronanet.ErrEventNotFound:
			http.Error(w, "Hosted event doesn't exist", http.StatusNotFound)
		case events.ErrEventConcluded:
			http.Error(w, "Hosted event already terminated", http.StatusForbidden)
		case nil:
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

// serveHostedEventBanner serves API calls concerning a hosted event's banner picture.
func (api *api) serveHostedEventBanner(w http.ResponseWriter, r *http.Request, uid tornet.IdentityFingerprint) {
	switch r.Method {
	case "GET":
		// Retrieves a hosted event's banner picture
		switch infos, err := api.backend.HostedEvent(uid); {
		case err == coronanet.ErrEventNotFound:
			http.Error(w, "Hosted event doesn't exist", http.StatusForbidden)
		case err == nil && infos.Banner == [32]byte{}:
			http.Error(w, "Hosted event doesn't have a banner picture", http.StatusNotFound)
		case err == nil:
			http.Redirect(w, r, fmt.Sprintf("/cdn/images/%x", infos.Banner), http.StatusFound)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	case "PUT":
		// Updates the hosted event's banner picture

		// Load the entire image into memory
		r.ParseMultipartForm(1 << 20) // 1MB max image size

		file, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		defer file.Close()

		var buffer bytes.Buffer
		io.Copy(&buffer, file)

		// Attempt to push the image into the database
		switch err := api.backend.UploadHostedEventBanner(uid, buffer.Bytes()); err {
		case coronanet.ErrEventNotFound:
			http.Error(w, "Hosted event doesn't exist", http.StatusForbidden)
		case events.ErrEventConcluded:
			http.Error(w, "Hosted event already terminated", http.StatusConflict)
		case nil:
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	case "DELETE":
		// Deletes the hosted event's banner picture
		switch err := api.backend.DeleteHostedEventBanner(uid); err {
		case coronanet.ErrEventNotFound:
			http.Error(w, "Hosted event doesn't exist", http.StatusForbidden)
		case nil:
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

// serveHostedEventCheckin serves API calls concerning a hosted event's checkin procedure.
func (api *api) serveHostedEventCheckin(w http.ResponseWriter, r *http.Request, uid tornet.IdentityFingerprint) {
	switch r.Method {
	case "POST":
		// Creates or retrieves the current checkin session
		switch session, err := api.backend.InitEventCheckin(uid); err {
		case coronanet.ErrNetworkDisabled:
			http.Error(w, "Cannot pair while offline", http.StatusForbidden)
		case coronanet.ErrEventNotFound:
			http.Error(w, "Hosted event doesn't exist", http.StatusForbidden)
		case nil:
			w.Header().Add("Content-Type", "application/json")
			json.NewEncoder(w).Encode(append(append(session.Identity, session.Address...), session.Auth...))
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	case "GET":
		// Waits for a checkin session to complete
		switch err := api.backend.WaitEventCheckin(uid); err {
		case coronanet.ErrCheckinNotInProgress:
			http.Error(w, "No checkin session in progress", http.StatusForbidden)
		case nil:
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

// serveJoinedEvents serves API calls concerning joined events.
func (api *api) serveJoinedEvents(w http.ResponseWriter, r *http.Request, path string) {
	// If we're not serving the events root, descend into a single event
	if path != "" {
		api.serveJoinedEvent(w, r, path)
		return
	}
	// Handle serving the events root
	switch r.Method {
	case "GET":
		// List all events joined by the local user
		w.Header().Add("Content-Type", "application/json")
		json.NewEncoder(w).Encode(api.backend.JoinedEvents())

	case "POST":
		// Checks into an existing event

		// Read the checkin secret from the request body
		var blob []byte
		if err := json.NewDecoder(r.Body).Decode(&blob); err != nil { // Bit unorthodox, but we don't want callers to interpret the data
			http.Error(w, "Provided checkin secret is invalid: "+err.Error(), http.StatusBadRequest)
			return
		}
		if len(blob) != 96 {
			http.Error(w, "Provided checkin secret is invalid: not 96 bytes", http.StatusBadRequest)
			return
		}
		switch err := api.backend.JoinEventCheckin(blob[:32], blob[32:64], blob[64:]); err {
		case coronanet.ErrNetworkDisabled:
			http.Error(w, "Cannot pair while offline", http.StatusForbidden)
		case coronanet.ErrEventAlreadyJoined:
			http.Error(w, "Remote event already joined", http.StatusConflict)
		case nil:
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

// serveJoinedEvent serves API calls concerning a single joined event.
func (api *api) serveJoinedEvent(w http.ResponseWriter, r *http.Request, path string) {
	// All event APIs need to provide the unique id
	parts := strings.SplitN(path[1:], "/", 2)

	uid := tornet.IdentityFingerprint(parts[0])
	path = ""
	if len(parts) > 1 {
		path = "/" + parts[1]
	}
	// If we're not serving the event root, descend further down
	if path != "" {
		switch {
		case strings.HasPrefix(path, "/banner"):
			api.serveJoinedEventBanner(w, r, uid)
		default:
			http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		}
		return
	}
	// Handle serving the event root
	switch r.Method {
	case "GET":
		// Retrieves a hosted event's statistics
		switch infos, err := api.backend.JoinedEvent(uid); err {
		case coronanet.ErrEventNotFound:
			http.Error(w, "Joined event doesn't exist", http.StatusNotFound)
		case nil:
			w.Header().Add("Content-Type", "application/json")
			json.NewEncoder(w).Encode(infos.Stats())
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

// serveJoinedEventBanner serves API calls concerning a joined event's picture.
func (api *api) serveJoinedEventBanner(w http.ResponseWriter, r *http.Request, uid tornet.IdentityFingerprint) {
	switch r.Method {
	case "GET":
		// Retrieves a hosted event's banner picture
		switch infos, err := api.backend.JoinedEvent(uid); {
		case err == coronanet.ErrEventNotFound:
			http.Error(w, "Joined event doesn't exist", http.StatusForbidden)
		case err == nil && infos.Banner == [32]byte{}:
			http.Error(w, "Joined event doesn't have a banner picture", http.StatusNotFound)
		case err == nil:
			http.Redirect(w, r, fmt.Sprintf("/cdn/images/%x", infos.Banner), http.StatusFound)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}
