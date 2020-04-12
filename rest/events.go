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
	"github.com/ethereum/go-ethereum/log"
)

// EventConfig is the initial configurations of an event when creating it.
type EventConfig struct {
	Name string `json:"name"`
}

// serveEvents serves API calls concerning all events.
func (api *api) serveEvents(w http.ResponseWriter, r *http.Request, path string, logger log.Logger) {
	switch {
	case strings.HasPrefix(path, "/hosted"):
		api.serveHostedEvents(w, r, strings.TrimPrefix(path, "/hosted"), logger)
	case strings.HasPrefix(path, "/joined"):
		api.serveJoinedEvents(w, r, strings.TrimPrefix(path, "/joined"), logger)
	default:
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
	}
}

// serveHostedEvents serves API calls concerning hosted events.
func (api *api) serveHostedEvents(w http.ResponseWriter, r *http.Request, path string, logger log.Logger) {
	// If we're not serving the events root, descend into a single event
	if path != "" {
		api.serveHostedEvent(w, r, path, logger)
		return
	}
	// Handle serving the events root
	switch r.Method {
	case "GET":
		// List all the hosted events
		logger.Debug("Requesting hosted event listing")
		w.Header().Add("Content-Type", "application/json")
		json.NewEncoder(w).Encode(api.backend.HostedEvents())

	case "POST":
		// Hosts a new event
		logger.Debug("Requesting hosted event creation")
		config := new(EventConfig)
		if err := json.NewDecoder(r.Body).Decode(config); err != nil {
			logger.Warn("Provided event config is invalid", "err", err)
			http.Error(w, "Provided event config is invalid: "+err.Error(), http.StatusBadRequest)
			return
		}
		switch uid, err := api.backend.CreateEvent(config.Name); err {
		case coronanet.ErrProfileNotFound:
			logger.Warn("Local user doesn't exist")
			http.Error(w, "Local user doesn't exist", http.StatusForbidden)
		case nil:
			logger.Debug("Hosted event successfully created", "id", uid)
			w.Header().Add("Content-Type", "application/json")
			json.NewEncoder(w).Encode(uid)
		default:
			logger.Error("Hosted event creation failed", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

// serveHostedEvent serves API calls concerning a single hosted events.
func (api *api) serveHostedEvent(w http.ResponseWriter, r *http.Request, path string, logger log.Logger) {
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
			api.serveHostedEventCheckin(w, r, uid, logger)
		default:
			http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		}
		return
	}
	// Handle serving the event root
	switch r.Method {
	case "GET":
		// Retrieves a hosted event's statistics
		logger.Debug("Requesting hosted event")
		switch infos, err := api.backend.HostedEvent(uid); err {
		case coronanet.ErrEventNotFound:
			logger.Warn("Hosted event doesn't exist")
			http.Error(w, "Hosted event doesn't exist", http.StatusNotFound)
		case nil:
			logger.Debug("Hosted event successfully retrieved", "stats", infos.Stats())
			w.Header().Add("Content-Type", "application/json")
			json.NewEncoder(w).Encode(infos.Stats())
		default:
			logger.Error("Hosted event retrieval failed", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	case "DELETE":
		// Terminates the event, will be cleaned up automatically
		logger.Debug("Requesting hosted event termination")
		switch err := api.backend.TerminateEvent(uid); err {
		case coronanet.ErrEventNotFound:
			logger.Warn("Hosted event doesn't exist")
			http.Error(w, "Hosted event doesn't exist", http.StatusNotFound)
		case events.ErrEventConcluded:
			logger.Warn("Hosted event already terminated")
			http.Error(w, "Hosted event already terminated", http.StatusForbidden)
		case nil:
			logger.Debug("Hosted event successfully terminated")
			w.WriteHeader(http.StatusOK)
		default:
			logger.Error("Hosted event termination failed", "err", err)
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
func (api *api) serveHostedEventCheckin(w http.ResponseWriter, r *http.Request, uid tornet.IdentityFingerprint, logger log.Logger) {
	switch r.Method {
	case "POST":
		// Creates or retrieves the current checkin session
		logger.Debug("Requesting checkin session creation")
		switch session, err := api.backend.InitEventCheckin(uid); err {
		case coronanet.ErrNetworkDisabled:
			logger.Warn("Cannot checkin while offline")
			http.Error(w, "Cannot checkin while offline", http.StatusForbidden)
		case coronanet.ErrEventNotFound:
			logger.Warn("Hosted event doesn't exist")
			http.Error(w, "Hosted event doesn't exist", http.StatusForbidden)
		case nil:
			logger.Debug("Checkin session successfully created")
			w.Header().Add("Content-Type", "application/json")
			json.NewEncoder(w).Encode(append(append(session.Identity, session.Address...), session.Auth...))
		default:
			logger.Error("Checkin session creation failed")
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	case "GET":
		// Waits for a checkin session to complete
		logger.Debug("Requesting checkin session waiting")
		switch err := api.backend.WaitEventCheckin(uid); err {
		case coronanet.ErrCheckinNotInProgress:
			logger.Warn("No checkin session in progress")
			http.Error(w, "No checkin session in progress", http.StatusForbidden)
		case nil:
			logger.Debug("Checkin session successfully waited")
			w.WriteHeader(http.StatusOK)
		default:
			logger.Error("Checkin session waiting failed", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

// serveJoinedEvents serves API calls concerning joined events.
func (api *api) serveJoinedEvents(w http.ResponseWriter, r *http.Request, path string, logger log.Logger) {
	// If we're not serving the events root, descend into a single event
	if path != "" {
		api.serveJoinedEvent(w, r, path, logger)
		return
	}
	// Handle serving the events root
	switch r.Method {
	case "GET":
		// List all events joined by the local user
		logger.Debug("Requesting joined event listing")
		w.Header().Add("Content-Type", "application/json")
		json.NewEncoder(w).Encode(api.backend.JoinedEvents())

	case "POST":
		// Checks into an existing event
		logger.Debug("Requesting checkin session joining")

		// Read the checkin secret from the request body
		var blob []byte
		if err := json.NewDecoder(r.Body).Decode(&blob); err != nil { // Bit unorthodox, but we don't want callers to interpret the data
			logger.Warn("Provided checkin secret is invalid", "err", err)
			http.Error(w, "Provided checkin secret is invalid: "+err.Error(), http.StatusBadRequest)
			return
		}
		if len(blob) != 96 {
			logger.Warn("Provided checkin secret is invalid: not 96 bytes")
			http.Error(w, "Provided checkin secret is invalid: not 96 bytes", http.StatusBadRequest)
			return
		}
		switch err := api.backend.JoinEventCheckin(blob[:32], blob[32:64], blob[64:]); err {
		case coronanet.ErrProfileNotFound:
			logger.Warn("Cannot checkin without profile")
			http.Error(w, "Cannot checkin without profile", http.StatusForbidden)
		case coronanet.ErrNetworkDisabled:
			logger.Warn("Cannot checkin while offline")
			http.Error(w, "Cannot checkin while offline", http.StatusForbidden)
		case coronanet.ErrEventAlreadyJoined:
			logger.Warn("Remote event already joined")
			http.Error(w, "Remote event already joined", http.StatusConflict)
		case nil:
			logger.Debug("Remote event joined successfully")
			w.WriteHeader(http.StatusOK)
		default:
			logger.Error("Remote event joining failed", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

// serveJoinedEvent serves API calls concerning a single joined event.
func (api *api) serveJoinedEvent(w http.ResponseWriter, r *http.Request, path string, logger log.Logger) {
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
		logger.Debug("Requesting joined event")
		switch infos, err := api.backend.JoinedEvent(uid); err {
		case coronanet.ErrEventNotFound:
			logger.Warn("Joined event doesn't exist")
			http.Error(w, "Joined event doesn't exist", http.StatusNotFound)
		case nil:
			logger.Debug("Joined event successfully retrieved")
			w.Header().Add("Content-Type", "application/json")
			json.NewEncoder(w).Encode(infos.Stats())
		default:
			logger.Error("Joined event retrieval failed", "err", err)
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
