// Package httpapi exposes the REST API and the WebSocket endpoint.
package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/rebel600/geofencing-realtime-alert-system/backend/internal/config"
	"github.com/rebel600/geofencing-realtime-alert-system/backend/internal/store"
	"github.com/rebel600/geofencing-realtime-alert-system/backend/internal/ws"
)

// Server wires handlers to their dependencies.
type Server struct {
	store   *store.Store
	hub     *ws.Hub
	log     *slog.Logger
	limiter *vehicleLimiter
	cfg     config.Config
}

// New builds the HTTP handler with all routes and middleware.
func New(cfg config.Config, st *store.Store, hub *ws.Hub, log *slog.Logger) http.Handler {
	s := &Server{store: st, hub: hub, log: log, cfg: cfg, limiter: newVehicleLimiter(cfg.LocationRPS, cfg.LocationBurst)}

	mux := http.NewServeMux()
	// Assessment endpoints — paths and methods exactly as specified.
	mux.HandleFunc("POST /geofences", s.createGeofence)
	mux.HandleFunc("GET /geofences", s.listGeofences)
	mux.HandleFunc("POST /vehicles", s.createVehicle)
	mux.HandleFunc("GET /vehicles", s.listVehicles)
	mux.HandleFunc("POST /vehicles/location", s.updateLocation)
	mux.HandleFunc("GET /vehicles/location/{vehicle_id}", s.getVehicleLocation)
	mux.HandleFunc("POST /alerts/configure", s.configureAlert)
	mux.HandleFunc("GET /alerts", s.listAlerts)
	mux.HandleFunc("GET /violations/history", s.violationHistory)
	mux.Handle("GET /ws/alerts", hub)

	// Additions (not part of the spec, do not alter spec endpoints).
	mux.HandleFunc("DELETE /alerts/{alert_id}", s.deactivateAlert)
	mux.HandleFunc("GET /alerts/recent", s.recentAlerts)
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("/", s.notFound)

	var h http.Handler = mux
	h = s.requireAPIKey(h)
	h = s.cors(h)
	h = s.recoverPanics(h)
	h = s.logRequests(h)
	return h
}
