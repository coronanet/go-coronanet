// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package rest

import (
	"encoding/json"
	"net/http"

	"github.com/coronanet/go-coronanet"
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

// serveGateway serves API calls concerning the P2P gateway.
func (api *api) serveGateway(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		// Retrieves the current status of the Corona Network gateway
		var (
			status gatewayStatus
			err    error
		)
		status.Enabled, status.Connected, status.Bandwidth.Ingress, status.Bandwidth.Egress, err = api.backend.Status()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// All ok, stream the status and stats over to the client
		w.Header().Add("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)

	case "PUT":
		// Requests the gateway to connect to the Corona Network
		switch err := api.backend.Enable(); err {
		case coronanet.ErrProfileNotFound:
			http.Error(w, "Cannot join the Corona Network without a local user", http.StatusForbidden)
		case nil:
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	case "DELETE":
		// Ping the backend to disable itself, don't care if it's running or not,
		// keeps things stateless
		if err := api.backend.Disable(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)

	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}
