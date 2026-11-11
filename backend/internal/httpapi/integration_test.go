package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/rebel600/geofencing-realtime-alert-system/backend/internal/config"
	"github.com/rebel600/geofencing-realtime-alert-system/backend/internal/httpapi"
	"github.com/rebel600/geofencing-realtime-alert-system/backend/internal/store"
	"github.com/rebel600/geofencing-realtime-alert-system/backend/internal/ws"
)

// End-to-end test against a real PostGIS. Run with:
//
//	TEST_DATABASE_URL=postgres://geo:geo@localhost:5433/geofencing?sslmode=disable go test ./...
func newTestServer(t *testing.T, cfg config.Config) *httptest.Server {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if cfg.AllowedOrigins == nil {
		cfg.AllowedOrigins = []string{"*"}
	}
	hub := ws.NewHub(log, httpapi.CheckOrigin(cfg.AllowedOrigins))
	stop := make(chan struct{})
	go hub.Run(stop)
	srv := httptest.NewServer(httpapi.New(cfg, st, hub, log))
	t.Cleanup(func() { srv.Close(); close(stop); st.Close() })
	return srv
}

type client struct {
	t   *testing.T
	url string
}

func (c client) do(method, path string, body any) (int, map[string]any) {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.url+path, r)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		c.t.Fatalf("%s %s: decode: %v", method, path, err)
	}
	if ts, ok := out["time_ns"].(string); !ok || ts == "" {
		c.t.Fatalf("%s %s: missing string time_ns in %v", method, path, out)
	}
	return resp.StatusCode, out
}

func (c client) must(status int, method, path string, body any) map[string]any {
	c.t.Helper()
	got, out := c.do(method, path, body)
	if got != status {
		c.t.Fatalf("%s %s: want %d, got %d: %v", method, path, status, got, out)
	}
	return out
}

// square returns a closed [lat,lng] ring of side `size` degrees at (lat,lng).
func square(lat, lng, size float64) [][]float64 {
	return [][]float64{{lat, lng}, {lat + size, lng}, {lat + size, lng + size}, {lat, lng + size}, {lat, lng}}
}

func TestEndToEnd(t *testing.T) {
	srv := newTestServer(t, config.Config{})
	c := client{t, srv.URL}

	// Random origin so repeated runs against the same DB don't overlap.
	baseLat, baseLng := rand.Float64()*100-50, rand.Float64()*300-150
	run := fmt.Sprint(rand.Int())

	// --- validation ---
	open := square(baseLat, baseLng, 1)[:4]
	c.must(400, "POST", "/geofences", map[string]any{"name": "x", "category": "delivery_zone", "coordinates": open})
	bowtie := [][]float64{{baseLat, baseLng}, {baseLat + 1, baseLng + 1}, {baseLat + 1, baseLng}, {baseLat, baseLng + 1}, {baseLat, baseLng}}
	out := c.must(400, "POST", "/geofences", map[string]any{"name": "x", "category": "delivery_zone", "coordinates": bowtie})
	if !strings.Contains(fmt.Sprint(out["message"]), "invalid polygon") {
		t.Fatalf("want self-intersection error, got %v", out)
	}
	c.must(400, "POST", "/geofences", map[string]any{"name": "x", "category": "parking", "coordinates": square(baseLat, baseLng, 1)})
	c.must(400, "POST", "/geofences", map[string]any{"name": "x", "category": "delivery_zone", "coordinates": square(baseLat, baseLng, 1), "extra": 1})

	// --- geofences: A (delivery) and B (restricted) side by side ---
	a := c.must(201, "POST", "/geofences", map[string]any{
		"name": "Zone A " + run, "description": "d", "category": "delivery_zone", "coordinates": square(baseLat, baseLng, 1)})
	b := c.must(201, "POST", "/geofences", map[string]any{
		"name": "Zone B " + run, "category": "restricted_zone", "coordinates": square(baseLat, baseLng+2, 1)})
	geoA, geoB := a["id"].(string), b["id"].(string)
	if !strings.HasPrefix(geoA, "geo_") || a["status"] != "active" {
		t.Fatalf("bad create geofence response %v", a)
	}
	list := c.must(200, "GET", "/geofences?category=restricted_zone", nil)
	for _, g := range list["geofences"].([]any) {
		if g.(map[string]any)["category"] != "restricted_zone" {
			t.Fatal("category filter leaked other categories")
		}
	}

	// --- vehicles ---
	v := c.must(201, "POST", "/vehicles", map[string]any{
		"vehicle_number": "T-" + run, "driver_name": "Jane", "vehicle_type": "truck", "phone": "+1234567890"})
	veh := v["id"].(string)
	c.must(409, "POST", "/vehicles", map[string]any{
		"vehicle_number": "T-" + run, "driver_name": "Jane", "vehicle_type": "truck", "phone": "+1234567890"})
	c.must(400, "POST", "/vehicles", map[string]any{"vehicle_number": "Z-" + run, "driver_name": "Jane", "vehicle_type": "truck"})

	// --- alert rules: entry on A (all vehicles), both on B (this vehicle) ---
	c.must(201, "POST", "/alerts/configure", map[string]any{"geofence_id": geoA, "event_type": "entry"})
	c.must(409, "POST", "/alerts/configure", map[string]any{"geofence_id": geoA, "event_type": "entry"})
	c.must(201, "POST", "/alerts/configure", map[string]any{"geofence_id": geoB, "vehicle_id": veh, "event_type": "both"})
	c.must(404, "POST", "/alerts/configure", map[string]any{"geofence_id": "geo_nope", "event_type": "entry"})
	c.must(404, "POST", "/alerts/configure", map[string]any{"geofence_id": geoA, "vehicle_id": "veh_nope", "event_type": "entry"})
	c.must(400, "POST", "/alerts/configure", map[string]any{"geofence_id": geoA, "event_type": "enter"})
	rules := c.must(200, "GET", "/alerts?geofence_id="+geoB, nil)["alerts"].([]any)
	if len(rules) != 1 || rules[0].(map[string]any)["vehicle_number"] != "T-"+run {
		t.Fatalf("bad alert rule listing: %v", rules)
	}

	// --- WebSocket subscriber ---
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/alerts", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	time.Sleep(50 * time.Millisecond) // let the hub register the client

	t0 := time.Now().UTC().Truncate(time.Second)
	ping := func(lat, lng float64, at time.Time) map[string]any {
		return c.must(200, "POST", "/vehicles/location", map[string]any{
			"vehicle_id": veh, "latitude": lat, "longitude": lng, "timestamp": at.Format(time.RFC3339)})
	}
	nextAlert := func() map[string]any {
		t.Helper()
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		_, msg, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("expected a WebSocket alert: %v", err)
		}
		var m map[string]any
		_ = json.Unmarshal(msg, &m)
		return m
	}

	// 1. outside everything: no geofences, no events
	r := ping(baseLat-1, baseLng-1, t0)
	if len(r["current_geofences"].([]any)) != 0 || len(r["events"].([]any)) != 0 {
		t.Fatalf("expected nothing, got %v", r)
	}

	// 2. enter A -> entry event + alert
	r = ping(baseLat+0.5, baseLng+0.5, t0.Add(time.Minute))
	cg := r["current_geofences"].([]any)
	if len(cg) != 1 || cg[0].(map[string]any)["geofence_id"] != geoA || cg[0].(map[string]any)["status"] != "inside" {
		t.Fatalf("expected inside A, got %v", r)
	}
	alert := nextAlert()
	if alert["event_type"] != "entry" || alert["geofence"].(map[string]any)["geofence_id"] != geoA ||
		alert["vehicle"].(map[string]any)["driver_name"] != "Jane" || !strings.HasPrefix(alert["event_id"].(string), "evt_") {
		t.Fatalf("bad entry alert %v", alert)
	}

	// 3. boundary point of A counts as inside (ST_Covers) -> no new event
	r = ping(baseLat, baseLng+0.5, t0.Add(2*time.Minute))
	if len(r["current_geofences"].([]any)) != 1 || len(r["events"].([]any)) != 0 {
		t.Fatalf("boundary should stay inside A with no event, got %v", r)
	}

	// 4. stale ping (older timestamp) into B: stored, but no state change / events
	r = ping(baseLat+0.5, baseLng+2.5, t0.Add(30*time.Second))
	if r["stale"] != true || len(r["events"].([]any)) != 0 {
		t.Fatalf("expected stale with no events, got %v", r)
	}

	// 5. jump A -> B: exit A (no rule matches exit on A) + entry B (rule 'both')
	r = ping(baseLat+0.5, baseLng+2.5, t0.Add(3*time.Minute))
	if n := len(r["events"].([]any)); n != 2 || r["alerts_triggered"].(float64) != 1 {
		t.Fatalf("expected 2 events / 1 alert, got %v", r)
	}
	alert = nextAlert()
	if alert["event_type"] != "entry" || alert["geofence"].(map[string]any)["category"] != "restricted_zone" {
		t.Fatalf("bad restricted entry alert %v", alert)
	}

	// 6. leave B -> exit alert (rule 'both')
	ping(baseLat-1, baseLng-1, t0.Add(4*time.Minute))
	if alert = nextAlert(); alert["event_type"] != "exit" {
		t.Fatalf("expected exit alert, got %v", alert)
	}

	// --- current location ---
	loc := c.must(200, "GET", "/vehicles/location/"+veh, nil)
	if len(loc["current_geofences"].([]any)) != 0 || loc["current_location"].(map[string]any)["latitude"].(float64) != baseLat-1 {
		t.Fatalf("bad current location %v", loc)
	}
	c.must(404, "GET", "/vehicles/location/veh_nope", nil)

	// --- history: entry A, exit A, entry B, exit B ---
	hist := c.must(200, "GET", "/violations/history?vehicle_id="+veh, nil)
	if hist["total_count"].(float64) != 4 {
		t.Fatalf("want 4 events, got %v", hist)
	}
	first := hist["violations"].([]any)[0].(map[string]any)
	if first["event_type"] != "exit" || first["geofence_id"] != geoB {
		t.Fatalf("history should be newest first, got %v", first)
	}
	page := c.must(200, "GET", "/violations/history?vehicle_id="+veh+"&geofence_id="+geoA+"&limit=1", nil)
	if page["total_count"].(float64) != 2 || len(page["violations"].([]any)) != 1 {
		t.Fatalf("bad filtered page %v", page)
	}
	window := c.must(200, "GET", "/violations/history?vehicle_id="+veh+"&start_date="+
		t0.Add(150*time.Second).Format(time.RFC3339)+"&end_date="+t0.Add(200*time.Second).Format(time.RFC3339), nil)
	if window["total_count"].(float64) != 2 { // exit A + entry B at t0+3m
		t.Fatalf("bad date window %v", window)
	}
	c.must(400, "GET", "/violations/history?start_date=nope", nil)
	c.must(400, "GET", "/violations/history?limit=0", nil)

	// --- validation on location update ---
	c.must(400, "POST", "/vehicles/location", map[string]any{"vehicle_id": veh, "latitude": 91, "longitude": 0, "timestamp": t0.Format(time.RFC3339)})
	c.must(400, "POST", "/vehicles/location", map[string]any{"vehicle_id": veh, "longitude": 0, "timestamp": t0.Format(time.RFC3339)})
	c.must(404, "POST", "/vehicles/location", map[string]any{"vehicle_id": "veh_nope", "latitude": 1, "longitude": 1, "timestamp": t0.Format(time.RFC3339)})
	c.must(404, "GET", "/nope", nil)
}

func TestAPIKeyAndRateLimit(t *testing.T) {
	srv := newTestServer(t, config.Config{APIKey: "secret", LocationRPS: 1, LocationBurst: 2})

	resp, err := http.Get(srv.URL + "/vehicles")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("want 401 without key, got %d", resp.StatusCode)
	}

	codes := []int{}
	for range 4 {
		req, _ := http.NewRequest("POST", srv.URL+"/vehicles/location",
			strings.NewReader(`{"vehicle_id":"veh_ratelimit","latitude":1,"longitude":1,"timestamp":"2025-01-01T00:00:00Z"}`))
		req.Header.Set("X-API-Key", "secret")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		codes = append(codes, resp.StatusCode)
	}
	// burst of 2 passes the limiter (then 404: vehicle doesn't exist), the rest are throttled
	if codes[0] != 404 || codes[1] != 404 || codes[2] != 429 || codes[3] != 429 {
		t.Fatalf("unexpected status sequence %v", codes)
	}
}
