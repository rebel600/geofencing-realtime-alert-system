package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/rebel600/geofencing-realtime-alert-system/backend/internal/domain"
)

// AlertMessage is the real-time alert payload (WebSocket message format from the spec).
type AlertMessage struct {
	EventID   string           `json:"event_id"`
	EventType domain.EventType `json:"event_type"`
	Timestamp time.Time        `json:"timestamp"`
	Vehicle   AlertVehicle     `json:"vehicle"`
	Geofence  GeofenceRef      `json:"geofence"`
	Location  AlertLocation    `json:"location"`
}

// AlertVehicle identifies the vehicle in an alert.
type AlertVehicle struct {
	ID            string `json:"vehicle_id"`
	VehicleNumber string `json:"vehicle_number"`
	DriverName    string `json:"driver_name"`
}

// AlertLocation is where the transition was detected.
type AlertLocation struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// LocationUpdate is the validated input to RecordLocation.
type LocationUpdate struct {
	VehicleID string
	Latitude  float64
	Longitude float64
	Timestamp time.Time
}

// LocationResult is what RecordLocation produced.
type LocationResult struct {
	CurrentGeofences []GeofenceRef
	Transitions      []Transition
	Alerts           []AlertMessage // to broadcast after commit
	Stale            bool           // timestamp older than the last accepted one
}

// Transition is one detected entry or exit.
type Transition struct {
	EventID  string           `json:"event_id"`
	Event    domain.EventType `json:"event_type"`
	Geofence GeofenceRef      `json:"geofence"`
}

// RecordLocation stores a location ping and, in the same transaction, detects
// geofence entry/exit transitions, records them, and records alerts for any
// matching rules. The returned Alerts are persisted but not yet delivered:
// the caller publishes them after this returns (i.e. after commit).
//
// The vehicle row is locked FOR UPDATE so concurrent updates for the same
// vehicle are serialised and cannot both emit the same transition.
// Pings older than the last accepted one are stored in history but do not
// change geofence state or emit events (out-of-order delivery protection).
func (s *Store) RecordLocation(ctx context.Context, in LocationUpdate) (LocationResult, error) {
	var res LocationResult

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return res, err
	}
	defer tx.Rollback(ctx)

	var vehicle AlertVehicle
	var lastRecorded *time.Time
	err = tx.QueryRow(ctx, `
		SELECT id, vehicle_number, driver_name, last_recorded_at
		FROM vehicles WHERE id = $1 FOR UPDATE`, in.VehicleID).
		Scan(&vehicle.ID, &vehicle.VehicleNumber, &vehicle.DriverName, &lastRecorded)
	if errors.Is(err, pgx.ErrNoRows) {
		return res, ErrUnknownVehicle
	}
	if err != nil {
		return res, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO vehicle_locations (vehicle_id, latitude, longitude, recorded_at)
		VALUES ($1, $2, $3, $4)`, in.VehicleID, in.Latitude, in.Longitude, in.Timestamp); err != nil {
		return res, err
	}

	res.CurrentGeofences, err = containingGeofences(ctx, tx, in.Latitude, in.Longitude)
	if err != nil {
		return res, err
	}

	if lastRecorded != nil && in.Timestamp.Before(*lastRecorded) {
		res.Stale = true
		return res, tx.Commit(ctx)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE vehicles SET last_latitude = $2, last_longitude = $3, last_recorded_at = $4
		WHERE id = $1`, in.VehicleID, in.Latitude, in.Longitude, in.Timestamp); err != nil {
		return res, err
	}

	// Diff previous state against current containment.
	previous, err := s.geofenceState(ctx, tx, in.VehicleID)
	if err != nil {
		return res, err
	}
	currentIDs := make([]string, len(res.CurrentGeofences))
	refs := make(map[string]GeofenceRef, len(res.CurrentGeofences)+len(previous))
	for i, g := range res.CurrentGeofences {
		currentIDs[i] = g.ID
		refs[g.ID] = g
	}
	previousIDs := make([]string, 0, len(previous))
	for id, g := range previous {
		previousIDs = append(previousIDs, id)
		if _, ok := refs[id]; !ok {
			refs[id] = g
		}
	}
	entered, exited := domain.DiffGeofences(previousIDs, currentIDs)
	if len(entered) == 0 && len(exited) == 0 {
		return res, tx.Commit(ctx)
	}

	if len(exited) > 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM vehicle_geofence_state WHERE vehicle_id = $1 AND geofence_id = ANY($2)`,
			in.VehicleID, exited); err != nil {
			return res, err
		}
	}
	if len(entered) > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO vehicle_geofence_state (vehicle_id, geofence_id, entered_at)
			SELECT $1, unnest($2::text[]), $3`, in.VehicleID, entered, in.Timestamp); err != nil {
			return res, err
		}
	}

	changed := append(append([]string{}, entered...), exited...)
	rules, err := activeRules(ctx, tx, in.VehicleID, changed)
	if err != nil {
		return res, err
	}

	record := func(geofenceID string, event domain.EventType) error {
		t := Transition{EventID: domain.NewID("viol"), Event: event, Geofence: refs[geofenceID]}
		if _, err := tx.Exec(ctx, `
			INSERT INTO geofence_events (id, vehicle_id, geofence_id, event_type, latitude, longitude, occurred_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			t.EventID, in.VehicleID, geofenceID, event, in.Latitude, in.Longitude, in.Timestamp); err != nil {
			return err
		}
		res.Transitions = append(res.Transitions, t)

		rule, ok := domain.FirstMatchingRule(rules, in.VehicleID, geofenceID, event)
		if !ok {
			return nil
		}
		msg := AlertMessage{
			EventID:   domain.NewID("evt"),
			EventType: event,
			Timestamp: in.Timestamp,
			Vehicle:   vehicle,
			Geofence:  t.Geofence,
			Location:  AlertLocation{Latitude: in.Latitude, Longitude: in.Longitude},
		}
		payload, err := json.Marshal(msg)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO alerts (id, event_id, rule_id, payload) VALUES ($1, $2, $3, $4)`,
			msg.EventID, t.EventID, rule.ID, payload); err != nil {
			return err
		}
		res.Alerts = append(res.Alerts, msg)
		return nil
	}

	// Exits before entries so a feed reads naturally when moving zone to zone.
	for _, id := range exited {
		if err := record(id, domain.EventExit); err != nil {
			return res, err
		}
	}
	for _, id := range entered {
		if err := record(id, domain.EventEntry); err != nil {
			return res, err
		}
	}
	return res, tx.Commit(ctx)
}

func (s *Store) geofenceState(ctx context.Context, tx pgx.Tx, vehicleID string) (map[string]GeofenceRef, error) {
	rows, err := tx.Query(ctx, `
		SELECT g.id, g.name, g.category FROM vehicle_geofence_state st
		JOIN geofences g ON g.id = st.geofence_id
		WHERE st.vehicle_id = $1`, vehicleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]GeofenceRef{}
	for rows.Next() {
		var g GeofenceRef
		if err := rows.Scan(&g.ID, &g.Name, &g.Category); err != nil {
			return nil, err
		}
		out[g.ID] = g
	}
	return out, rows.Err()
}

func activeRules(ctx context.Context, tx pgx.Tx, vehicleID string, geofenceIDs []string) ([]domain.AlertRule, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, geofence_id, vehicle_id, event_type FROM alert_rules
		WHERE status = 'active' AND geofence_id = ANY($1) AND (vehicle_id IS NULL OR vehicle_id = $2)
		ORDER BY created_at, id`, geofenceIDs, vehicleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.AlertRule
	for rows.Next() {
		var r domain.AlertRule
		var event string
		if err := rows.Scan(&r.ID, &r.GeofenceID, &r.VehicleID, &event); err != nil {
			return nil, err
		}
		r.EventType = domain.RuleEventType(event)
		out = append(out, r)
	}
	return out, rows.Err()
}
