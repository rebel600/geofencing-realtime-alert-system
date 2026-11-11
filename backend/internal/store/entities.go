package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/rebel600/geofencing-realtime-alert-system/backend/internal/domain"
)

// ---------- Geofences ----------

// Geofence is a stored polygonal zone.
type Geofence struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Coordinates [][]float64 `json:"coordinates"`
	Category    string      `json:"category"`
	Status      string      `json:"status"`
	CreatedAt   time.Time   `json:"created_at"`
}

// NewGeofence is the validated input for CreateGeofence.
type NewGeofence struct {
	Name        string
	Description string
	Coordinates [][]float64
	Category    domain.Category
}

// CreateGeofence validates the polygon topology in PostGIS and inserts it.
func (s *Store) CreateGeofence(ctx context.Context, in NewGeofence) (Geofence, error) {
	wkt := domain.PolygonWKT(in.Coordinates)

	var valid bool
	var reason string
	err := s.pool.QueryRow(ctx,
		`SELECT ST_IsValid(g), ST_IsValidReason(g) FROM ST_GeomFromText($1, 4326) AS g`, wkt).
		Scan(&valid, &reason)
	if err != nil {
		return Geofence{}, fmt.Errorf("validate polygon: %w", err)
	}
	if !valid {
		return Geofence{}, &domain.ValidationError{Field: "coordinates", Message: "invalid polygon: " + reason}
	}

	coordsJSON, err := json.Marshal(in.Coordinates)
	if err != nil {
		return Geofence{}, err
	}
	g := Geofence{
		ID:          domain.NewID("geo"),
		Name:        in.Name,
		Description: in.Description,
		Coordinates: in.Coordinates,
		Category:    string(in.Category),
		Status:      "active",
	}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO geofences (id, name, description, category, coordinates, geom, status)
		VALUES ($1, $2, $3, $4, $5, ST_GeomFromText($6, 4326), $7)
		RETURNING created_at`,
		g.ID, g.Name, g.Description, g.Category, coordsJSON, wkt, g.Status).Scan(&g.CreatedAt)
	return g, err
}

// ListGeofences returns geofences (newest first), optionally filtered by category.
func (s *Store) ListGeofences(ctx context.Context, category string, page Page) ([]Geofence, int, error) {
	var w where
	if category != "" {
		w.add("category = $%d", category)
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM geofences`+w.String(), w.args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args := append([]any{}, w.args...)
	q := `SELECT id, name, description, coordinates, category, status, created_at FROM geofences` +
		w.String() + ` ORDER BY created_at DESC, id` + page.sql(&args)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Geofence{}
	for rows.Next() {
		var g Geofence
		var coords []byte
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &coords, &g.Category, &g.Status, &g.CreatedAt); err != nil {
			return nil, 0, err
		}
		if err := json.Unmarshal(coords, &g.Coordinates); err != nil {
			return nil, 0, err
		}
		out = append(out, g)
	}
	return out, total, rows.Err()
}

// ---------- Vehicles ----------

// Location is a timestamped position.
type Location struct {
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	Timestamp time.Time `json:"timestamp"`
}

// Vehicle is a registered vehicle.
type Vehicle struct {
	ID              string    `json:"id"`
	VehicleNumber   string    `json:"vehicle_number"`
	DriverName      string    `json:"driver_name"`
	VehicleType     string    `json:"vehicle_type"`
	Phone           string    `json:"phone"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
	CurrentLocation *Location `json:"current_location"`
}

// NewVehicle is the validated input for CreateVehicle.
type NewVehicle struct {
	VehicleNumber, DriverName, VehicleType, Phone string
}

// CreateVehicle inserts a vehicle; returns ErrConflict if vehicle_number is taken.
func (s *Store) CreateVehicle(ctx context.Context, in NewVehicle) (Vehicle, error) {
	v := Vehicle{
		ID:            domain.NewID("veh"),
		VehicleNumber: in.VehicleNumber,
		DriverName:    in.DriverName,
		VehicleType:   in.VehicleType,
		Phone:         in.Phone,
		Status:        "active",
	}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO vehicles (id, vehicle_number, driver_name, vehicle_type, phone, status)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING created_at`,
		v.ID, v.VehicleNumber, v.DriverName, v.VehicleType, v.Phone, v.Status).Scan(&v.CreatedAt)
	if isUniqueViolation(err) {
		return Vehicle{}, ErrConflict
	}
	return v, err
}

const vehicleColumns = `id, vehicle_number, driver_name, vehicle_type, phone, status, created_at,
	last_latitude, last_longitude, last_recorded_at`

func scanVehicle(row pgx.Row) (Vehicle, error) {
	var v Vehicle
	var lat, lng *float64
	var ts *time.Time
	if err := row.Scan(&v.ID, &v.VehicleNumber, &v.DriverName, &v.VehicleType, &v.Phone, &v.Status,
		&v.CreatedAt, &lat, &lng, &ts); err != nil {
		return Vehicle{}, err
	}
	if lat != nil && lng != nil && ts != nil {
		v.CurrentLocation = &Location{Latitude: *lat, Longitude: *lng, Timestamp: ts.UTC()}
	}
	return v, nil
}

// ListVehicles returns vehicles (newest first) with their last known location.
func (s *Store) ListVehicles(ctx context.Context, page Page) ([]Vehicle, int, error) {
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM vehicles`).Scan(&total); err != nil {
		return nil, 0, err
	}
	var args []any
	rows, err := s.pool.Query(ctx, `SELECT `+vehicleColumns+` FROM vehicles ORDER BY created_at DESC, id`+page.sql(&args), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Vehicle{}
	for rows.Next() {
		v, err := scanVehicle(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}

// GetVehicle fetches one vehicle or ErrNotFound.
func (s *Store) GetVehicle(ctx context.Context, id string) (Vehicle, error) {
	v, err := scanVehicle(s.pool.QueryRow(ctx, `SELECT `+vehicleColumns+` FROM vehicles WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Vehicle{}, ErrNotFound
	}
	return v, err
}

// GeofenceRef is a geofence summary used in location responses and alerts.
type GeofenceRef struct {
	ID       string `json:"geofence_id"`
	Name     string `json:"geofence_name"`
	Category string `json:"category"`
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// containingGeofences runs the point-in-polygon query. ST_Covers (unlike
// ST_Contains) treats points on the boundary as inside. The GIST index on
// geom makes this an index scan rather than a test against every polygon.
func containingGeofences(ctx context.Context, q querier, lat, lng float64) ([]GeofenceRef, error) {
	rows, err := q.Query(ctx, `
		SELECT id, name, category FROM geofences
		WHERE status = 'active' AND ST_Covers(geom, ST_SetSRID(ST_MakePoint($1, $2), 4326))
		ORDER BY name, id`, lng, lat)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GeofenceRef{}
	for rows.Next() {
		var g GeofenceRef
		if err := rows.Scan(&g.ID, &g.Name, &g.Category); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// GeofencesAt returns the active geofences that cover a point.
func (s *Store) GeofencesAt(ctx context.Context, lat, lng float64) ([]GeofenceRef, error) {
	return containingGeofences(ctx, s.pool, lat, lng)
}

// ---------- Alert rules ----------

// AlertRule is a configured alert rule joined with display names.
type AlertRule struct {
	ID            string    `json:"alert_id"`
	GeofenceID    string    `json:"geofence_id"`
	GeofenceName  string    `json:"geofence_name"`
	VehicleID     *string   `json:"vehicle_id"`
	VehicleNumber *string   `json:"vehicle_number"`
	EventType     string    `json:"event_type"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
}

// NewAlertRule is the validated input for CreateAlertRule.
type NewAlertRule struct {
	GeofenceID string
	VehicleID  *string
	EventType  domain.RuleEventType
}

// ErrUnknownGeofence / ErrUnknownVehicle distinguish which reference was missing.
var (
	ErrUnknownGeofence = fmt.Errorf("geofence %w", ErrNotFound)
	ErrUnknownVehicle  = fmt.Errorf("vehicle %w", ErrNotFound)
)

// CreateAlertRule inserts a rule after checking referenced rows exist.
func (s *Store) CreateAlertRule(ctx context.Context, in NewAlertRule) (AlertRule, error) {
	r := AlertRule{ID: domain.NewID("alert"), GeofenceID: in.GeofenceID, VehicleID: in.VehicleID,
		EventType: string(in.EventType), Status: "active"}

	if err := s.pool.QueryRow(ctx, `SELECT name FROM geofences WHERE id = $1`, in.GeofenceID).Scan(&r.GeofenceName); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AlertRule{}, ErrUnknownGeofence
		}
		return AlertRule{}, err
	}
	if in.VehicleID != nil {
		var num string
		if err := s.pool.QueryRow(ctx, `SELECT vehicle_number FROM vehicles WHERE id = $1`, *in.VehicleID).Scan(&num); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return AlertRule{}, ErrUnknownVehicle
			}
			return AlertRule{}, err
		}
		r.VehicleNumber = &num
	}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO alert_rules (id, geofence_id, vehicle_id, event_type, status)
		VALUES ($1, $2, $3, $4, $5) RETURNING created_at`,
		r.ID, r.GeofenceID, r.VehicleID, r.EventType, r.Status).Scan(&r.CreatedAt)
	if isUniqueViolation(err) {
		return AlertRule{}, ErrConflict
	}
	return r, err
}

// ListAlertRules returns rules (newest first) with optional filters.
func (s *Store) ListAlertRules(ctx context.Context, geofenceID, vehicleID string, page Page) ([]AlertRule, int, error) {
	var w where
	if geofenceID != "" {
		w.add("r.geofence_id = $%d", geofenceID)
	}
	if vehicleID != "" {
		w.add("r.vehicle_id = $%d", vehicleID)
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM alert_rules r`+w.String(), w.args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args := append([]any{}, w.args...)
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.geofence_id, g.name, r.vehicle_id, v.vehicle_number, r.event_type, r.status, r.created_at
		FROM alert_rules r
		JOIN geofences g ON g.id = r.geofence_id
		LEFT JOIN vehicles v ON v.id = r.vehicle_id`+w.String()+
		` ORDER BY r.created_at DESC, r.id`+page.sql(&args), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []AlertRule{}
	for rows.Next() {
		var r AlertRule
		if err := rows.Scan(&r.ID, &r.GeofenceID, &r.GeofenceName, &r.VehicleID, &r.VehicleNumber,
			&r.EventType, &r.Status, &r.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// DeactivateAlertRule marks a rule inactive (kept for audit). ErrNotFound if missing.
func (s *Store) DeactivateAlertRule(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE alert_rules SET status = 'inactive' WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------- Violation history ----------

// Violation is a stored entry/exit event.
type Violation struct {
	ID            string    `json:"id"`
	VehicleID     string    `json:"vehicle_id"`
	VehicleNumber string    `json:"vehicle_number"`
	GeofenceID    string    `json:"geofence_id"`
	GeofenceName  string    `json:"geofence_name"`
	Category      string    `json:"category"`
	EventType     string    `json:"event_type"`
	Latitude      float64   `json:"latitude"`
	Longitude     float64   `json:"longitude"`
	Timestamp     time.Time `json:"timestamp"`
}

// ViolationFilter narrows GET /violations/history.
type ViolationFilter struct {
	VehicleID, GeofenceID string
	Start, End            *time.Time
	Page                  Page
}

// ListViolations returns events newest first plus the total matching count.
func (s *Store) ListViolations(ctx context.Context, f ViolationFilter) ([]Violation, int, error) {
	var w where
	if f.VehicleID != "" {
		w.add("e.vehicle_id = $%d", f.VehicleID)
	}
	if f.GeofenceID != "" {
		w.add("e.geofence_id = $%d", f.GeofenceID)
	}
	if f.Start != nil {
		w.add("e.occurred_at >= $%d", *f.Start)
	}
	if f.End != nil {
		w.add("e.occurred_at <= $%d", *f.End)
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM geofence_events e`+w.String(), w.args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args := append([]any{}, w.args...)
	rows, err := s.pool.Query(ctx, `
		SELECT e.id, e.vehicle_id, v.vehicle_number, e.geofence_id, g.name, g.category,
		       e.event_type, e.latitude, e.longitude, e.occurred_at
		FROM geofence_events e
		JOIN vehicles v ON v.id = e.vehicle_id
		JOIN geofences g ON g.id = e.geofence_id`+w.String()+
		` ORDER BY e.occurred_at DESC, e.id DESC`+f.Page.sql(&args), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Violation{}
	for rows.Next() {
		var v Violation
		if err := rows.Scan(&v.ID, &v.VehicleID, &v.VehicleNumber, &v.GeofenceID, &v.GeofenceName, &v.Category,
			&v.EventType, &v.Latitude, &v.Longitude, &v.Timestamp); err != nil {
			return nil, 0, err
		}
		v.Timestamp = v.Timestamp.UTC()
		out = append(out, v)
	}
	return out, total, rows.Err()
}

// RecentAlerts returns the most recent delivered alert payloads (newest first),
// used by the frontend to back-fill its live feed after (re)connecting.
func (s *Store) RecentAlerts(ctx context.Context, limit int) ([]json.RawMessage, error) {
	rows, err := s.pool.Query(ctx, `SELECT payload FROM alerts ORDER BY created_at DESC, id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var p []byte
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
