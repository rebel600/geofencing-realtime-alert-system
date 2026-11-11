package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/rebel600/geofencing-realtime-alert-system/backend/internal/domain"
	"github.com/rebel600/geofencing-realtime-alert-system/backend/internal/store"
)

// storeError maps persistence errors to HTTP responses.
func (s *Server) storeError(w http.ResponseWriter, start time.Time, err error) {
	switch {
	case domain.IsValidation(err):
		writeValidation(w, start, err)
	case errors.Is(err, store.ErrUnknownGeofence):
		writeError(w, start, http.StatusNotFound, "not_found", "geofence not found")
	case errors.Is(err, store.ErrUnknownVehicle):
		writeError(w, start, http.StatusNotFound, "not_found", "vehicle not found")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, start, http.StatusNotFound, "not_found", "resource not found")
	default:
		s.log.Error("store error", "err", err)
		writeError(w, start, http.StatusInternalServerError, "internal_error", "unexpected server error")
	}
}

func required(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return &domain.ValidationError{Field: field, Message: "is required"}
	}
	return nil
}

// ---------- Geofences ----------

type createGeofenceRequest struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Coordinates [][]float64 `json:"coordinates"`
	Category    string      `json:"category"`
}

func (s *Server) createGeofence(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	var req createGeofenceRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeValidation(w, start, err)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if err := required("name", req.Name); err != nil {
		writeValidation(w, start, err)
		return
	}
	if err := required("category", req.Category); err != nil {
		writeValidation(w, start, err)
		return
	}
	cat := domain.Category(req.Category)
	if !cat.Valid() {
		writeValidation(w, start, &domain.ValidationError{Field: "category",
			Message: "must be one of delivery_zone, restricted_zone, toll_zone, customer_area"})
		return
	}
	if err := domain.ValidatePolygon(req.Coordinates); err != nil {
		writeValidation(w, start, err)
		return
	}

	g, err := s.store.CreateGeofence(r.Context(), store.NewGeofence{
		Name: req.Name, Description: strings.TrimSpace(req.Description), Coordinates: req.Coordinates, Category: cat,
	})
	if err != nil {
		s.storeError(w, start, err)
		return
	}
	writeJSON(w, start, http.StatusCreated, map[string]any{
		"id": g.ID, "name": g.Name, "status": g.Status,
		"description": g.Description, "category": g.Category, "coordinates": g.Coordinates, "created_at": g.CreatedAt,
	})
}

func (s *Server) listGeofences(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	category := r.URL.Query().Get("category")
	if category != "" && !domain.Category(category).Valid() {
		writeValidation(w, start, &domain.ValidationError{Field: "category",
			Message: "must be one of delivery_zone, restricted_zone, toll_zone, customer_area"})
		return
	}
	limit, offset, err := parsePage(r, 0, 1000)
	if err != nil {
		writeValidation(w, start, err)
		return
	}
	items, total, err := s.store.ListGeofences(r.Context(), category, store.Page{Limit: limit, Offset: offset})
	if err != nil {
		s.storeError(w, start, err)
		return
	}
	writeJSON(w, start, http.StatusOK, map[string]any{"geofences": items, "total_count": total})
}

// ---------- Vehicles ----------

type createVehicleRequest struct {
	VehicleNumber string `json:"vehicle_number"`
	DriverName    string `json:"driver_name"`
	VehicleType   string `json:"vehicle_type"`
	Phone         string `json:"phone"`
}

func (s *Server) createVehicle(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	var req createVehicleRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeValidation(w, start, err)
		return
	}
	in := store.NewVehicle{
		VehicleNumber: strings.TrimSpace(req.VehicleNumber),
		DriverName:    strings.TrimSpace(req.DriverName),
		VehicleType:   strings.TrimSpace(req.VehicleType),
		Phone:         strings.TrimSpace(req.Phone),
	}
	for _, f := range []struct{ name, value string }{
		{"vehicle_number", in.VehicleNumber}, {"driver_name", in.DriverName},
		{"vehicle_type", in.VehicleType}, {"phone", in.Phone},
	} {
		if err := required(f.name, f.value); err != nil {
			writeValidation(w, start, err)
			return
		}
	}
	if !validPhone(in.Phone) {
		writeValidation(w, start, &domain.ValidationError{Field: "phone",
			Message: "must contain 7-15 digits, optionally starting with + (spaces and dashes allowed)"})
		return
	}

	v, err := s.store.CreateVehicle(r.Context(), in)
	if errors.Is(err, store.ErrConflict) {
		writeError(w, start, http.StatusConflict, "conflict", "a vehicle with this vehicle_number already exists")
		return
	}
	if err != nil {
		s.storeError(w, start, err)
		return
	}
	writeJSON(w, start, http.StatusCreated, map[string]any{
		"id": v.ID, "vehicle_number": v.VehicleNumber, "status": v.Status,
		"driver_name": v.DriverName, "vehicle_type": v.VehicleType, "phone": v.Phone, "created_at": v.CreatedAt,
	})
}

func validPhone(p string) bool {
	digits := 0
	for i, c := range p {
		switch {
		case c >= '0' && c <= '9':
			digits++
		case c == '+' && i == 0, c == ' ', c == '-', c == '(', c == ')':
		default:
			return false
		}
	}
	return digits >= 7 && digits <= 15
}

func (s *Server) listVehicles(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	limit, offset, err := parsePage(r, 0, 1000)
	if err != nil {
		writeValidation(w, start, err)
		return
	}
	items, total, err := s.store.ListVehicles(r.Context(), store.Page{Limit: limit, Offset: offset})
	if err != nil {
		s.storeError(w, start, err)
		return
	}
	writeJSON(w, start, http.StatusOK, map[string]any{"vehicles": items, "total_count": total})
}

// ---------- Locations ----------

type updateLocationRequest struct {
	VehicleID string   `json:"vehicle_id"`
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
	Timestamp string   `json:"timestamp"`
}

type geofenceStatus struct {
	ID       string `json:"geofence_id"`
	Name     string `json:"geofence_name"`
	Category string `json:"category"`
	Status   string `json:"status"`
}

func (s *Server) updateLocation(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	var req updateLocationRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeValidation(w, start, err)
		return
	}
	if err := required("vehicle_id", req.VehicleID); err != nil {
		writeValidation(w, start, err)
		return
	}
	if req.Latitude == nil {
		writeValidation(w, start, &domain.ValidationError{Field: "latitude", Message: "is required"})
		return
	}
	if req.Longitude == nil {
		writeValidation(w, start, &domain.ValidationError{Field: "longitude", Message: "is required"})
		return
	}
	if err := domain.ValidateLatLng(*req.Latitude, *req.Longitude); err != nil {
		writeValidation(w, start, err)
		return
	}
	ts, err := domain.ParseTimestamp("timestamp", req.Timestamp)
	if err != nil {
		writeValidation(w, start, err)
		return
	}
	if !s.limiter.Allow(req.VehicleID) {
		w.Header().Set("Retry-After", "1")
		writeError(w, start, http.StatusTooManyRequests, "rate_limited", "too many location updates for this vehicle; slow down")
		return
	}

	res, err := s.store.RecordLocation(r.Context(), store.LocationUpdate{
		VehicleID: req.VehicleID, Latitude: *req.Latitude, Longitude: *req.Longitude, Timestamp: ts,
	})
	if err != nil {
		s.storeError(w, start, err)
		return
	}

	// Alerts are already committed; delivery is asynchronous and never blocks this response.
	for _, a := range res.Alerts {
		s.hub.Publish(a)
	}

	current := make([]geofenceStatus, len(res.CurrentGeofences))
	for i, g := range res.CurrentGeofences {
		current[i] = geofenceStatus{ID: g.ID, Name: g.Name, Category: g.Category, Status: "inside"}
	}
	transitions := res.Transitions
	if transitions == nil {
		transitions = []store.Transition{}
	}
	writeJSON(w, start, http.StatusOK, map[string]any{
		"vehicle_id":        req.VehicleID,
		"location_updated":  true,
		"current_geofences": current,
		"events":            transitions,
		"alerts_triggered":  len(res.Alerts),
		"stale":             res.Stale,
	})
}

func (s *Server) getVehicleLocation(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	v, err := s.store.GetVehicle(r.Context(), r.PathValue("vehicle_id"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			err = store.ErrUnknownVehicle
		}
		s.storeError(w, start, err)
		return
	}
	geofences := []store.GeofenceRef{}
	if v.CurrentLocation != nil {
		geofences, err = s.store.GeofencesAt(r.Context(), v.CurrentLocation.Latitude, v.CurrentLocation.Longitude)
		if err != nil {
			s.storeError(w, start, err)
			return
		}
	}
	writeJSON(w, start, http.StatusOK, map[string]any{
		"vehicle_id":        v.ID,
		"vehicle_number":    v.VehicleNumber,
		"current_location":  v.CurrentLocation, // null if the vehicle has never reported
		"current_geofences": geofences,
	})
}

// ---------- Alerts ----------

type configureAlertRequest struct {
	GeofenceID string  `json:"geofence_id"`
	VehicleID  *string `json:"vehicle_id"`
	EventType  string  `json:"event_type"`
}

func (s *Server) configureAlert(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	var req configureAlertRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeValidation(w, start, err)
		return
	}
	if err := required("geofence_id", req.GeofenceID); err != nil {
		writeValidation(w, start, err)
		return
	}
	if err := required("event_type", req.EventType); err != nil {
		writeValidation(w, start, err)
		return
	}
	ev := domain.RuleEventType(req.EventType)
	if !ev.Valid() {
		writeValidation(w, start, &domain.ValidationError{Field: "event_type", Message: "must be one of entry, exit, both"})
		return
	}
	if req.VehicleID != nil && strings.TrimSpace(*req.VehicleID) == "" {
		req.VehicleID = nil // "" is treated as "all vehicles"
	}

	rule, err := s.store.CreateAlertRule(r.Context(), store.NewAlertRule{
		GeofenceID: req.GeofenceID, VehicleID: req.VehicleID, EventType: ev,
	})
	if errors.Is(err, store.ErrConflict) {
		writeError(w, start, http.StatusConflict, "conflict", "an identical active alert rule already exists")
		return
	}
	if err != nil {
		s.storeError(w, start, err)
		return
	}
	writeJSON(w, start, http.StatusCreated, rule)
}

func (s *Server) listAlerts(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	limit, offset, err := parsePage(r, 0, 1000)
	if err != nil {
		writeValidation(w, start, err)
		return
	}
	q := r.URL.Query()
	items, total, err := s.store.ListAlertRules(r.Context(), q.Get("geofence_id"), q.Get("vehicle_id"),
		store.Page{Limit: limit, Offset: offset})
	if err != nil {
		s.storeError(w, start, err)
		return
	}
	writeJSON(w, start, http.StatusOK, map[string]any{"alerts": items, "total_count": total})
}

func (s *Server) deactivateAlert(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	id := r.PathValue("alert_id")
	if err := s.store.DeactivateAlertRule(r.Context(), id); err != nil {
		s.storeError(w, start, err)
		return
	}
	writeJSON(w, start, http.StatusOK, map[string]any{"alert_id": id, "status": "inactive"})
}

func (s *Server) recentAlerts(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	limit, _, err := parsePage(r, 50, 200)
	if err != nil {
		writeValidation(w, start, err)
		return
	}
	items, err := s.store.RecentAlerts(r.Context(), limit)
	if err != nil {
		s.storeError(w, start, err)
		return
	}
	writeJSON(w, start, http.StatusOK, map[string]any{"alerts": items})
}

// ---------- Violations ----------

func (s *Server) violationHistory(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	q := r.URL.Query()
	limit, offset, err := parsePage(r, 50, 500)
	if err != nil {
		writeValidation(w, start, err)
		return
	}
	f := store.ViolationFilter{
		VehicleID: q.Get("vehicle_id"), GeofenceID: q.Get("geofence_id"),
		Page: store.Page{Limit: limit, Offset: offset},
	}
	if v := q.Get("start_date"); v != "" {
		t, err := domain.ParseTimestamp("start_date", v)
		if err != nil {
			writeValidation(w, start, err)
			return
		}
		f.Start = &t
	}
	if v := q.Get("end_date"); v != "" {
		t, err := domain.ParseTimestamp("end_date", v)
		if err != nil {
			writeValidation(w, start, err)
			return
		}
		f.End = &t
	}
	if f.Start != nil && f.End != nil && f.End.Before(*f.Start) {
		writeValidation(w, start, &domain.ValidationError{Field: "end_date", Message: "must not be before start_date"})
		return
	}
	items, total, err := s.store.ListViolations(r.Context(), f)
	if err != nil {
		s.storeError(w, start, err)
		return
	}
	writeJSON(w, start, http.StatusOK, map[string]any{
		"violations": items, "total_count": total, "limit": limit, "offset": offset,
	})
}

// ---------- Misc ----------

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if err := s.store.Ping(r.Context()); err != nil {
		writeError(w, start, http.StatusServiceUnavailable, "unavailable", "database unreachable")
		return
	}
	writeJSON(w, start, http.StatusOK, map[string]any{"status": "ok", "ws_clients": s.hub.Clients()})
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, time.Now(), http.StatusNotFound, "not_found", "no route for "+r.Method+" "+r.URL.Path)
}
