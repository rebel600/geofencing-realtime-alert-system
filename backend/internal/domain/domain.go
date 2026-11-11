// Package domain holds the core types and pure business rules of the
// geofencing system: validation, entry/exit detection and alert-rule matching.
// Nothing in here touches the database or the network, so it is fully unit-testable.
package domain

import (
	"crypto/rand"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Category is the kind of zone a geofence represents.
type Category string

const (
	CategoryDelivery   Category = "delivery_zone"
	CategoryRestricted Category = "restricted_zone"
	CategoryToll       Category = "toll_zone"
	CategoryCustomer   Category = "customer_area"
)

// Categories lists every accepted geofence category.
var Categories = []Category{CategoryDelivery, CategoryRestricted, CategoryToll, CategoryCustomer}

// Valid reports whether c is one of the accepted categories.
func (c Category) Valid() bool {
	for _, known := range Categories {
		if c == known {
			return true
		}
	}
	return false
}

// EventType is a geofence transition.
type EventType string

const (
	EventEntry EventType = "entry"
	EventExit  EventType = "exit"
)

// RuleEventType is what an alert rule listens for: entry, exit or both.
type RuleEventType string

const (
	RuleEntry RuleEventType = "entry"
	RuleExit  RuleEventType = "exit"
	RuleBoth  RuleEventType = "both"
)

// Valid reports whether t is entry, exit or both.
func (t RuleEventType) Valid() bool {
	return t == RuleEntry || t == RuleExit || t == RuleBoth
}

// Matches reports whether a rule of type t fires for event e.
func (t RuleEventType) Matches(e EventType) bool {
	return t == RuleBoth || string(t) == string(e)
}

// ValidationError is returned for bad client input; handlers map it to HTTP 400.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Field == "" {
		return e.Message
	}
	return e.Field + ": " + e.Message
}

func invalid(field, format string, args ...any) error {
	return &ValidationError{Field: field, Message: fmt.Sprintf(format, args...)}
}

// IsValidation reports whether err is (or wraps) a ValidationError.
func IsValidation(err error) bool {
	var ve *ValidationError
	return errors.As(err, &ve)
}

// ValidateLatLng checks that a latitude/longitude pair is within WGS84 bounds.
func ValidateLatLng(lat, lng float64) error {
	if lat < -90 || lat > 90 {
		return invalid("latitude", "must be between -90 and 90, got %v", lat)
	}
	if lng < -180 || lng > 180 {
		return invalid("longitude", "must be between -180 and 180, got %v", lng)
	}
	return nil
}

// ValidatePolygon checks a ring of [latitude, longitude] pairs:
// every pair has two in-range values, the ring is closed, and it has
// at least 4 points of which at least 3 are distinct.
// Self-intersection is checked later by PostGIS (ST_IsValid).
func ValidatePolygon(coords [][]float64) error {
	if len(coords) < 4 {
		return invalid("coordinates", "need at least 4 points (3 unique + 1 closing point), got %d", len(coords))
	}
	for i, p := range coords {
		if len(p) != 2 {
			return invalid("coordinates", "point %d must be a [latitude, longitude] pair", i)
		}
		if err := ValidateLatLng(p[0], p[1]); err != nil {
			return invalid("coordinates", "point %d: %v", i, err)
		}
	}
	first, last := coords[0], coords[len(coords)-1]
	if first[0] != last[0] || first[1] != last[1] {
		return invalid("coordinates", "polygon must be closed: first and last points must be identical")
	}
	unique := make(map[[2]float64]struct{}, len(coords))
	for _, p := range coords[:len(coords)-1] {
		unique[[2]float64{p[0], p[1]}] = struct{}{}
	}
	if len(unique) < 3 {
		return invalid("coordinates", "polygon needs at least 3 unique points, got %d", len(unique))
	}
	return nil
}

// PolygonWKT renders a [lat, lng] ring as WKT. WKT (and PostGIS) use
// x=longitude, y=latitude, so the pair order is swapped here — this is
// the single place in the codebase where that conversion happens.
func PolygonWKT(coords [][]float64) string {
	var b strings.Builder
	b.WriteString("POLYGON((")
	for i, p := range coords {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%v %v", p[1], p[0])
	}
	b.WriteString("))")
	return b.String()
}

// ParseTimestamp parses an ISO 8601 / RFC 3339 timestamp and normalises it to UTC.
func ParseTimestamp(field, value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, invalid(field, "is required (ISO 8601, e.g. 2025-01-15T10:35:00Z)")
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, invalid(field, "must be an ISO 8601 timestamp (e.g. 2025-01-15T10:35:00Z)")
	}
	return t.UTC(), nil
}

// DiffGeofences compares the geofences a vehicle was inside before and is
// inside now, returning the ones it entered and the ones it exited (sorted).
func DiffGeofences(previous, current []string) (entered, exited []string) {
	prev := make(map[string]struct{}, len(previous))
	for _, id := range previous {
		prev[id] = struct{}{}
	}
	curr := make(map[string]struct{}, len(current))
	for _, id := range current {
		curr[id] = struct{}{}
		if _, ok := prev[id]; !ok {
			entered = append(entered, id)
		}
	}
	for id := range prev {
		if _, ok := curr[id]; !ok {
			exited = append(exited, id)
		}
	}
	sort.Strings(entered)
	sort.Strings(exited)
	return entered, exited
}

// AlertRule is the subset of a configured rule needed for matching.
type AlertRule struct {
	ID         string
	GeofenceID string
	VehicleID  *string // nil means "all vehicles"
	EventType  RuleEventType
}

// RuleMatches reports whether rule fires for the given vehicle/geofence/event.
func RuleMatches(rule AlertRule, vehicleID, geofenceID string, event EventType) bool {
	if rule.GeofenceID != geofenceID {
		return false
	}
	if rule.VehicleID != nil && *rule.VehicleID != vehicleID {
		return false
	}
	return rule.EventType.Matches(event)
}

// FirstMatchingRule returns the first rule that fires, so an event produces
// at most one alert even when several rules overlap.
func FirstMatchingRule(rules []AlertRule, vehicleID, geofenceID string, event EventType) (AlertRule, bool) {
	for _, r := range rules {
		if RuleMatches(r, vehicleID, geofenceID, event) {
			return r, true
		}
	}
	return AlertRule{}, false
}

const idAlphabet = "0123456789abcdefghijklmnopqrstuvwxyz"

// NewID returns prefix + "_" + 12 random base-36 characters, e.g. "geo_k3j9x0a2bq7m".
func NewID(prefix string) string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	for i, b := range buf {
		buf[i] = idAlphabet[int(b)%len(idAlphabet)]
	}
	return prefix + "_" + string(buf)
}
