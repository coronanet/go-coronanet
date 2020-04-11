// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package events

import (
	"fmt"
	"time"

	"github.com/coronanet/go-coronanet/params"
)

// Stats is a collection of public statistics about an event.
type Stats struct {
	Name  string    `json:"name"`  // Name of the event
	Start time.Time `json:"start"` // Start time of the event
	End   time.Time `json:"end"`   // Conclusion time of the event

	Attendees uint `json:"attendees"` // Number of participants in the event
	Negatives uint `json:"negatives"` // Participants who reported negative test results
	Suspected uint `json:"suspected"` // Participants who might have been infected
	Positives uint `json:"positives"` // Participants who reported positive infection

	Updated time.Time `json:"updated"` // Time when the event was last modified
	Synced  time.Time `json:"synced"`  // Time when the event was last synced
}

// Stats converts an internal event configuration into an external stats dump.
func (s *ServerInfos) Stats() *Stats {
	stats := &Stats{
		Name:      s.Name,
		Start:     s.Start,
		End:       s.End,
		Attendees: uint(len(s.Participants)),
		Updated:   s.Updated,
		Synced:    time.Now(),
	}
	for _, status := range s.Statuses {
		switch status {
		case params.InfectionStatusNegative:
			stats.Negatives++
		case params.InfectionStatusSuspected:
			stats.Suspected++
		case params.InfectionStatusPositive:
			stats.Positives++
		case params.InfectionStatusUnknown:
		// Do nothing
		default:
			panic(fmt.Sprintf("unknown infection status: %s", status))
		}
	}
	// Merge the organizer into the attendees too
	stats.Attendees++

	return stats
}

// Stats converts an internal event configuration into an external stats dump.
func (c *ClientInfos) Stats() *Stats {
	return &Stats{
		Name:      c.Name,
		Start:     c.Start,
		End:       c.End,
		Attendees: c.Attendees,
		Negatives: c.Negatives,
		Suspected: c.Suspected,
		Positives: c.Positives,
		Updated:   c.Updated,
		Synced:    c.Synced,
	}
}
