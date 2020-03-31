// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

// This file contains a development server to launch a local coronanet instance
// without all the mobile integration.

package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/coronanet/go-coronanet"
	"github.com/coronanet/go-coronanet/rest"
	"github.com/ethereum/go-ethereum/log"
)

var (
	datadirFlag   = flag.String("datadir", ".", "Data directory for the backend")
	apiportFlag   = flag.Int("apiport", 4444, "TCP port to launch the API server on")
	verbosityFlag = flag.Int("verbosity", int(log.LvlInfo), "Log level to run with")
)

func main() {
	flag.Parse()

	// Enable colored terminal logging
	log.Root().SetHandler(log.LvlFilterHandler(log.Lvl(*verbosityFlag), log.StreamHandler(os.Stderr, log.TerminalFormat(true))))

	// Create a live backend and expose via REST
	backend, err := coronanet.NewBackend(*datadirFlag)
	if err != nil {
		panic(err)
	}
	defer backend.Close()

	http.ListenAndServe(fmt.Sprintf("localhost:%d", *apiportFlag), rest.New(backend))
}
