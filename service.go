// coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package coronanet

import (
	"net/http"
	"strings"
)

// ServeHTTP implements http.Handler, serving API calls from the mobile UI. It
// exposes all the functionality of the social network via a RESTful interface
// for react native on a mobile.
func (b *Backend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasPrefix(r.URL.Path, "/gateway"):
		b.serveHTTPGateway(w, r)
	default:
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
	}
}
