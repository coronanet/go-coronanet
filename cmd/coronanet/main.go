// coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

// This file contains a development server to launch a local coronanet instance
// without all the mobile integration.

package main

import (
	"flag"
	"fmt"
	"net/http"

	"github.com/coronanet/go-coronanet"
)

var portFlag = flag.Int("port", 4444, "TCP port to launch the API server on")

func main() {
	flag.Parse()
	http.ListenAndServe(fmt.Sprintf("localhost:%d", *portFlag), new(coronanet.Backend))
}
