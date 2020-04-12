// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package rest

import (
	"encoding/json"
	"net/http"

	"github.com/ethereum/go-ethereum/log"
)

// GatewayStatus is the response struct sent back to the client when requesting
// the current status of the Corona Network P2P gateway.
type GatewayStatus struct {
	Enabled   bool `json:"enabled"`
	Connected bool `json:"connected"`
	Bandwidth struct {
		Ingress uint64 `json:"ingress"`
		Egress  uint64 `json:"egress"`
	} `json:"bandwidth"`
}

// serveGateway serves API calls concerning the P2P gateway.
func (api *api) serveGateway(w http.ResponseWriter, r *http.Request, logger log.Logger) {
	switch r.Method {
	case "GET":
		// Retrieves the current status of the Corona Network gateway
		logger.Trace("Retrieving gateway status")
		var (
			status GatewayStatus
			err    error
		)
		status.Enabled, status.Connected, status.Bandwidth.Ingress, status.Bandwidth.Egress, err = api.backend.GatewayStatus()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// All ok, stream the status and stats over to the client
		w.Header().Add("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)

	case "PUT":
		// Requests the gateway to connect to the Corona Network
		if err := api.backend.EnableGateway(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)

	case "DELETE":
		// Ping the backend to disable itself, don't care if it's running or not,
		// keeps things stateless
		if err := api.backend.DisableGateway(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)

	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}
