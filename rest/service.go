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
	default:
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
	}
}
