// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package rest

import (
	"encoding/json"
	"net/http"

	"github.com/coronanet/go-coronanet"
	"github.com/ethereum/go-ethereum/log"
)

// servePairing serves API calls concerning the contact pairing.
func (api *api) servePairing(w http.ResponseWriter, r *http.Request, logger log.Logger) {
	switch r.Method {
	case "POST":
		// Creates a pairing session for contact establishment
		logger.Debug("Requesting pairing session creation")
		switch secret, address, err := api.backend.InitPairing(); err {
		case coronanet.ErrProfileNotFound:
			logger.Warn("Cannot pair without profile")
			http.Error(w, "Cannot pair without profile", http.StatusForbidden)
		case coronanet.ErrNetworkDisabled:
			logger.Warn("Cannot pair while offline")
			http.Error(w, "Cannot pair while offline", http.StatusForbidden)
		case coronanet.ErrAlreadyPairing:
			logger.Warn("Pairing session already in progress")
			http.Error(w, "Pairing session already in progress", http.StatusForbidden)
		case nil:
			logger.Debug("Pairing session successfully created", "secret", secret.Fingerprint(), "address", address.Fingerprint())
			w.Header().Add("Content-Type", "application/json")
			json.NewEncoder(w).Encode(append(secret, address...))
		default:
			logger.Error("Pairing session creation failed", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	case "GET":
		// Waits for a pairing session to complete
		logger.Debug("Requesting waiting for pairing session")
		switch id, err := api.backend.WaitPairing(); err {
		case coronanet.ErrNotPairing:
			logger.Warn("No pairing session in progress")
			http.Error(w, "No pairing session in progress", http.StatusForbidden)
		case nil:
			// Pairing succeeded, try to inject the contact into the backend
			logger.Debug("Pairing wait completed successfully", "contact", id.Identity.Fingerprint(), "address", id.Address.Fingerprint())
			switch uid, err := api.backend.AddContact(id); err {
			case coronanet.ErrContactExists:
				http.Error(w, "Remote contact already paired", http.StatusConflict)
			case nil:
				w.Header().Add("Content-Type", "application/json")
				json.NewEncoder(w).Encode(uid)
			default:
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		default:
			logger.Error("Pairing session waiting failed", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	case "PUT":
		// Joins a pairing session for contact establishment
		logger.Debug("Requesting pairing session joining")

		// Read the pairing secret from the request body
		var blob []byte
		if err := json.NewDecoder(r.Body).Decode(&blob); err != nil { // Bit unorthodox, but we don't want callers to interpret the data
			logger.Error("Provided pairing secret is invalid", "err", err)
			http.Error(w, "Provided pairing secret is invalid: "+err.Error(), http.StatusBadRequest)
			return
		}
		if len(blob) != 64 {
			logger.Error("Provided pairing secret is invalid: not 64 bytes")
			http.Error(w, "Provided pairing secret is invalid: not 64 bytes", http.StatusBadRequest)
			return
		}
		switch id, err := api.backend.JoinPairing(blob[:32], blob[32:]); err {
		case coronanet.ErrProfileNotFound:
			logger.Warn("Cannot pair without profile")
			http.Error(w, "Cannot pair without profile", http.StatusForbidden)
		case coronanet.ErrNetworkDisabled:
			logger.Warn("Cannot pair while offline")
			http.Error(w, "Cannot pair while offline", http.StatusForbidden)
		case nil:
			// Pairing succeeded, try to inject the contact into the backend
			logger.Debug("Pairing join completed successfully", "contact", id.Identity.Fingerprint(), "address", id.Address.Fingerprint())
			switch uid, err := api.backend.AddContact(id); err {
			case coronanet.ErrContactExists:
				http.Error(w, "Remote contact already paired", http.StatusConflict)
			case nil:
				w.Header().Add("Content-Type", "application/json")
				json.NewEncoder(w).Encode(uid)
			default:
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		default:
			logger.Error("Pairing session joining failed", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}
