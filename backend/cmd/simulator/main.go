// Command simulator seeds demo data through the public API and then drives
// vehicles along looping routes so geofence entries/exits (and alerts) happen live.
//
//	go run ./cmd/simulator -api http://localhost:8080            # seed (if empty) + drive
//	go run ./cmd/simulator -api http://localhost:8080 -seed=false -interval 1s
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"log"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"
)

type zone struct {
	Name, Description, Category string
	Coordinates                 [][2]float64
	Rule                        string // alert rule event_type for all vehicles ("" = none)
}

// Demo layout around downtown San Francisco (same area as the spec examples).
var zones = []zone{
	{"Downtown Delivery Zone", "Main delivery area for downtown customers", "delivery_zone", rect(37.7749, -122.4194, 37.7849, -122.4094), "both"},
	{"Federal Building Perimeter", "No commercial vehicles", "restricted_zone", rect(37.7795, -122.4150, 37.7830, -122.4110), "entry"},
	{"Bay Bridge Toll Plaza", "Toll charged on entry", "toll_zone", rect(37.7860, -122.3950, 37.7900, -122.3880), "entry"},
	{"SoMa Customer Hub", "Key customer cluster", "customer_area", rect(37.7700, -122.4060, 37.7760, -122.3980), "exit"},
}

type vehicle struct {
	Number, Driver, Type, Phone string
	Route                       [][2]float64 // waypoints, looped
}

var vehicles = []vehicle{
	{"KA-01-AB-1234", "John Doe", "truck", "+14155550101", [][2]float64{
		{37.7720, -122.4230}, {37.7800, -122.4170}, {37.7812, -122.4130}, {37.7880, -122.4060}, {37.7880, -122.3920}, {37.7720, -122.4010}}},
	{"CA-7XYZ-552", "Priya Sharma", "van", "+14155550102", [][2]float64{
		{37.7730, -122.3950}, {37.7740, -122.4100}, {37.7820, -122.4180}, {37.7900, -122.4000}}},
	{"NY-MOTO-77", "Alex Kim", "motorcycle", "+14155550103", [][2]float64{
		{37.7680, -122.4120}, {37.7815, -122.4125}, {37.7890, -122.3910}, {37.7750, -122.3990}}},
}

func rect(lat1, lng1, lat2, lng2 float64) [][2]float64 {
	return [][2]float64{{lat1, lng1}, {lat2, lng1}, {lat2, lng2}, {lat1, lng2}, {lat1, lng1}}
}

type client struct {
	base, key string
	http      *http.Client
}

func (c client) call(method, path string, body, out any) (int, error) {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, err := http.NewRequest(method, c.base+path, &buf)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.key != "" {
		req.Header.Set("X-API-Key", c.key)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if out != nil {
		_ = json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode, nil
}

func main() {
	api := flag.String("api", envOr("API_URL", "http://localhost:8080"), "API base URL")
	key := flag.String("api-key", os.Getenv("API_KEY"), "API key, if the server requires one")
	seed := flag.Bool("seed", true, "create demo geofences, vehicles and alert rules if missing")
	interval := flag.Duration("interval", 2*time.Second, "time between location updates per vehicle")
	stepMeters := flag.Float64("step", 120, "metres travelled per update")
	flag.Parse()

	c := client{base: strings.TrimRight(*api, "/"), key: *key, http: &http.Client{Timeout: 10 * time.Second}}

	ids := map[string]string{} // vehicle_number -> id
	if *seed {
		seedData(c, ids)
	}
	var list struct {
		Vehicles []struct {
			ID            string `json:"id"`
			VehicleNumber string `json:"vehicle_number"`
		} `json:"vehicles"`
	}
	if _, err := c.call("GET", "/vehicles", nil, &list); err != nil {
		log.Fatalf("list vehicles: %v", err)
	}
	for _, v := range list.Vehicles {
		ids[v.VehicleNumber] = v.ID
	}

	type runner struct {
		v         vehicle
		id        string
		leg       int
		progress  float64 // metres along current leg
		lastZones string
	}
	var runners []*runner
	for _, v := range vehicles {
		if id, ok := ids[v.Number]; ok {
			runners = append(runners, &runner{v: v, id: id})
		}
	}
	if len(runners) == 0 {
		log.Fatal("no demo vehicles found; run with -seed")
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	tick := time.NewTicker(*interval)
	defer tick.Stop()
	log.Printf("driving %d vehicles every %s (Ctrl+C to stop)", len(runners), *interval)

	for {
		for _, r := range runners {
			route := r.v.Route
			a, b := route[r.leg], route[(r.leg+1)%len(route)]
			legLen := distance(a, b)
			r.progress += *stepMeters
			if r.progress >= legLen {
				r.progress -= legLen
				r.leg = (r.leg + 1) % len(route)
				a, b = route[r.leg], route[(r.leg+1)%len(route)]
				legLen = distance(a, b)
			}
			t := math.Min(1, r.progress/legLen)
			lat, lng := a[0]+(b[0]-a[0])*t, a[1]+(b[1]-a[1])*t

			var res struct {
				CurrentGeofences []struct {
					Name string `json:"geofence_name"`
				} `json:"current_geofences"`
				Events []struct {
					Type     string `json:"event_type"`
					Geofence struct {
						Name string `json:"geofence_name"`
					} `json:"geofence"`
				} `json:"events"`
				Error string `json:"message"`
			}
			status, err := c.call("POST", "/vehicles/location", map[string]any{
				"vehicle_id": r.id, "latitude": round6(lat), "longitude": round6(lng),
				"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
			}, &res)
			switch {
			case err != nil:
				log.Printf("%s: %v", r.v.Number, err)
			case status != 200:
				log.Printf("%s: HTTP %d %s", r.v.Number, status, res.Error)
			default:
				for _, e := range res.Events {
					log.Printf("%-14s %-5s %s", r.v.Number, strings.ToUpper(e.Type), e.Geofence.Name)
				}
			}
		}
		select {
		case <-stop:
			return
		case <-tick.C:
		}
	}
}

func seedData(c client, ids map[string]string) {
	var existing struct {
		Geofences []struct{ ID, Name string } `json:"geofences"`
	}
	if _, err := c.call("GET", "/geofences", nil, &existing); err != nil {
		log.Fatalf("cannot reach API at %s: %v", c.base, err)
	}
	have := map[string]string{}
	for _, g := range existing.Geofences {
		have[g.Name] = g.ID
	}
	for _, z := range zones {
		if _, ok := have[z.Name]; ok {
			continue
		}
		var out struct{ ID string }
		status, err := c.call("POST", "/geofences", map[string]any{
			"name": z.Name, "description": z.Description, "category": z.Category, "coordinates": z.Coordinates,
		}, &out)
		if err != nil || status != 201 {
			log.Fatalf("create geofence %q: status %d err %v", z.Name, status, err)
		}
		have[z.Name] = out.ID
		log.Printf("created geofence %-28s %s", z.Name, out.ID)
		if z.Rule != "" {
			c.call("POST", "/alerts/configure", map[string]any{"geofence_id": out.ID, "event_type": z.Rule}, nil)
			log.Printf("  alert rule: %s (all vehicles)", z.Rule)
		}
	}
	for _, v := range vehicles {
		var out struct{ ID string }
		status, err := c.call("POST", "/vehicles", map[string]any{
			"vehicle_number": v.Number, "driver_name": v.Driver, "vehicle_type": v.Type, "phone": v.Phone,
		}, &out)
		if err != nil {
			log.Fatalf("create vehicle: %v", err)
		}
		if status == 201 {
			ids[v.Number] = out.ID
			log.Printf("registered vehicle %-14s %s", v.Number, out.ID)
		}
	}
}

// distance is the haversine distance in metres.
func distance(a, b [2]float64) float64 {
	const r = 6371000
	toRad := math.Pi / 180
	dLat, dLng := (b[0]-a[0])*toRad, (b[1]-a[1])*toRad
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(a[0]*toRad)*math.Cos(b[0]*toRad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * r * math.Asin(math.Sqrt(h))
}

func round6(f float64) float64 { return math.Round(f*1e6) / 1e6 }

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
