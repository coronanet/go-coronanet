// coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package coronanet

import (
	"encoding/json"
	"net/http"
)

// gatewayStatus is the response struct sent back to the client when requesting
// the current status of the Corona Network P2P gateway.
type gatewayStatus struct {
	Enabled   bool `json:"enabled"`
	Connected bool `json:"connected"`
	Bandwidth struct {
		Ingress uint64 `json:"ingress"`
		Egress  uint64 `json:"egress"`
	} `json:"bandwidth"`
}

// serveHTTPGateway serves API calls concerning the P2P gateway.
func (b *Backend) serveHTTPGateway(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		// Retrieve the gateway status, stuff it into the response and abort if
		// something went horribly wrong.
		var (
			status gatewayStatus
			err    error
		)
		status.Enabled, status.Connected, status.Bandwidth.Ingress, status.Bandwidth.Egress, err = b.Status()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// All ok, stream the status and stats over to the client
		w.Header().Add("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)

	case "PUT":
		// Ping the backend to enable itself, don't care if it's running or not,
		// keeps things stateless
		if err := b.Enable(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)

	case "DELETE":
		// Ping the backend to disable itself, don't care if it's running or not,
		// keeps things stateless
		if err := b.Disable(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)

	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}
