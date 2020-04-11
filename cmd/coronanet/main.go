// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

// This file contains a development server to launch a local coronanet instance
// without all the mobile integration.

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/coronanet/go-coronanet"
	"github.com/coronanet/go-coronanet/rest"
	"github.com/ethereum/go-ethereum/log"
)

var (
	datadirFlag   = flag.String("datadir", "", "Data directory for the backend (default = temporary)")
	apiportFlag   = flag.Int("apiport", 0, "API listener port for the backend (default = automatic")
	hostnameFlag  = flag.String("hostname", "", "Optional hostname for extra logging context")
	verbosityFlag = flag.Int("verbosity", int(log.LvlInfo), "Log level to run with")
)

func main() {
	flag.Parse()

	// Enable colored terminal logging
	log.Root().SetHandler(log.LvlFilterHandler(log.Lvl(*verbosityFlag), log.StreamHandler(os.Stderr, log.TerminalFormat(true))))

	// Create a live backend and expose via REST
	if *datadirFlag == "" {
		datadir, err := ioutil.TempDir("", "")
		if err != nil {
			panic(err)
		}
		defer os.RemoveAll(datadir)

		*datadirFlag = datadir
	}
	backend, err := coronanet.NewBackend(*datadirFlag)
	if err != nil {
		panic(err)
	}
	defer backend.Close()

	// Manually create the API listener so we can capture port 0
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *apiportFlag))
	if err != nil {
		panic(err)
	}
	if err := ioutil.WriteFile(filepath.Join(*datadirFlag, "apiport"), []byte(strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)), 0600); err != nil {
		panic(err)
	}
	defer os.Remove(filepath.Join(*datadirFlag, "apiport"))

	// Capture interrupts and clean up the backend
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGABRT) // Everything else gets hogged by Tor, lame
	go func() {
		<-ch
		listener.Close()
	}()
	// Everything prepared, run the API server
	http.Serve(listener, rest.New(*hostnameFlag, backend))
}
