// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

// Package rest implements the RESTful API for the Corona Network.
package rest

import (
	"net/http"
	"strings"

	"github.com/coronanet/go-coronanet"
)

// New creates an REST API interface in front of a Corona Network backend.
func New(backend *coronanet.Backend) http.Handler {
	return &api{
		backend: backend,
	}
}

// api is a REST wrapper on top of the Corona Network backend that translates the
// Go APIs into REST according to the Swagger specs.
type api struct {
	backend *coronanet.Backend
}

// ServeHTTP implements http.Handler, serving API calls from the mobile UI. It
// exposes all the functionality of the social network via a RESTful interface
// for react native on a mobile.
func (api *api) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasPrefix(r.URL.Path, "/gateway"):
		api.serveGateway(w, r)
	case strings.HasPrefix(r.URL.Path, "/profile"):
		api.serveProfile(w, r, strings.TrimPrefix(r.URL.Path, "/profile"))
	case strings.HasPrefix(r.URL.Path, "/pairing"):
		api.servePairing(w, r)
	case strings.HasPrefix(r.URL.Path, "/contacts"):
		api.serveContacts(w, r, strings.TrimPrefix(r.URL.Path, "/contacts"))
	case strings.HasPrefix(r.URL.Path, "/events"):
		api.serveEvents(w, r, strings.TrimPrefix(r.URL.Path, "/events"))
	case strings.HasPrefix(r.URL.Path, "/cdn"):
		api.serveCDN(w, r, strings.TrimPrefix(r.URL.Path, "/cdn"))
	default:
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
	}
}
