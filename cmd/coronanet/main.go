// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

// This file contains a development server to launch a local coronanet instance
// without all the mobile integration.

package main

import (
	"flag"
	"fmt"
	"net/http"

	"github.com/coronanet/go-coronanet"
	"github.com/coronanet/go-coronanet/rest"
)

var (
	datadirFlag = flag.String("datadir", ".", "Data directory for the backend")
	portFlag    = flag.Int("port", 4444, "TCP port to launch the API server on")
)

func main() {
	flag.Parse()

	backend, err := coronanet.NewBackend(*datadirFlag)
	if err != nil {
		panic(err)
	}
	http.ListenAndServe(fmt.Sprintf("localhost:%d", *portFlag), rest.New(backend))
}
