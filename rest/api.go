// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package rest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/coronanet/go-coronanet/protocols/events"
)

// API is a tiny Go client for the Corona Network REST APIs. The purpose is to
// allow writing integration tests and scenarios in Go.
type API struct {
	endpoint string
}

// NewAPI creates a simplistic REST API around a Corona Network endpoint.
func NewAPI(endpoint string) *API {
	return &API{
		endpoint: endpoint,
	}
}

func (api *API) GatewayStatus() (*GatewayStatus, error) {
	status := new(GatewayStatus)
	if err := api.run("GET", "/gateway", nil, status); err != nil {
		return nil, err
	}
	return status, nil
}
func (api *API) EnableGateway() error {
	return api.run("PUT", "/gateway", nil, nil)
}
func (api *API) DisableGateway() error {
	return api.run("DELETE", "/gateway", nil, nil)
}

func (api *API) CreateProfile() error {
	return api.run("POST", "/profile", nil, nil)
}
func (api *API) Profile() (*ProfileInfos, error) {
	profile := new(ProfileInfos)
	if err := api.run("GET", "/profile", nil, profile); err != nil {
		return nil, err
	}
	return profile, nil
}
func (api *API) UpdateProfile(profile *ProfileInfos) error {
	return api.run("PUT", "/profile", profile, nil)
}
func (api *API) DeleteProfile() error { return api.run("DELETE", "/profile", nil, nil) }

func (api *API) InitPairing() (string, error) {
	var secret string
	if err := api.run("POST", "/pairing", nil, &secret); err != nil {
		return "", err
	}
	return secret, nil
}
func (api *API) JoinPairing(secret string) (string, error) {
	var contact string
	if err := api.run("PUT", "/pairing", secret, &contact); err != nil {
		return "", err
	}
	return contact, nil
}
func (api *API) WaitPairing() (string, error) {
	var contact string
	if err := api.run("GET", "/pairing", nil, &contact); err != nil {
		return "", err
	}
	return contact, nil
}

func (api *API) HostedEvents() ([]string, error) {
	var events []string
	if err := api.run("GET", "/events/hosted", nil, &events); err != nil {
		return nil, err
	}
	return events, nil
}
func (api *API) CreateEvent(config *EventConfig) (string, error) {
	var event string
	if err := api.run("POST", "/events/hosted", config, &event); err != nil {
		return "", err
	}
	return event, nil
}
func (api *API) HostedEvent(id string) (*events.Stats, error) {
	stats := new(events.Stats)
	if err := api.run("GET", "/events/hosted/"+id, nil, stats); err != nil {
		return nil, err
	}
	return stats, nil
}
func (api *API) TerminateEvent(id string) error {
	return api.run("DELETE", "/events/hosted/"+id, nil, nil)
}
func (api *API) InitEventCheckin(id string) (string, error) {
	var secret string
	if err := api.run("POST", "/events/hosted/"+id+"/checkin", nil, &secret); err != nil {
		return "", err
	}
	return secret, nil
}
func (api *API) WaitEventCheckin(id string) error {
	return api.run("GET", "/events/hosted/"+id+"/checkin", nil, nil)
}
func (api *API) JoinEventCheckin(secret string) error {
	return api.run("POST", "/events/joined", secret, nil)
}
func (api *API) JoinedEvents() ([]string, error) {
	var events []string
	if err := api.run("GET", "/events/joined", nil, &events); err != nil {
		return nil, err
	}
	return events, nil
}
func (api *API) JoinedEvent(id string) (*events.Stats, error) {
	stats := new(events.Stats)
	if err := api.run("GET", "/events/joined/"+id, nil, stats); err != nil {
		return nil, err
	}
	return stats, nil
}

// run creates an API requests of the given type and sends over a JSON encoded
// request, potentially expecting a reply, and converting any failures into a
// Go error.
func (api *API) run(method string, path string, request interface{}, reply interface{}) error {
	// If a request body was specified, serialized it
	var body []byte
	if request != nil {
		blob, err := json.Marshal(request)
		if err != nil {
			return err
		}
		body = blob
	}
	// Run the request and ensure it succeeds
	req, err := http.NewRequest(method, api.endpoint+path, bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	req.Header.Add("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	body, err = ioutil.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode != 200 {
		return fmt.Errorf("request failed: %d: %s", res.StatusCode, string(body))
	}
	// Request seems to have succeeded, parse any expected reply
	if reply != nil {
		return json.Unmarshal(body, reply)
	}
	return nil
}
