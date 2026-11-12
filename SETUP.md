# Setup Guide

Geofencing & Real-time Alert System: Go API + PostgreSQL/PostGIS + WebSocket alerts + React dashboard.

```
geofencing-realtime-alert-system/
├── backend/                 Go API (module: .../backend)
│   ├── cmd/api/             HTTP + WebSocket server entrypoint
│   ├── cmd/simulator/       Seeds demo data and drives vehicles along routes
│   ├── internal/domain/     Pure business rules: validation, entry/exit diff, rule matching (unit tested)
│   ├── internal/store/      PostGIS persistence + embedded SQL migrations
│   ├── internal/httpapi/    Handlers, middleware (CORS, API key, rate limit), e2e tests
│   ├── internal/ws/         WebSocket hub (broadcast to all connected clients)
│   └── Dockerfile
├── frontend/                React 19 + Vite + TypeScript + Tailwind + Leaflet
└── docker-compose.yml       db (PostGIS) + api + web
```

---

## 1. Prerequisites

| Tool | Version | Needed for |
|---|---|---|
| Docker + Docker Compose | Docker 24+, Compose v2 | Running everything |
| Go | 1.26+ | Backend dev / tests / simulator (optional if you only use Docker) |
| Node.js | 20+ (24 recommended) | Frontend dev (optional if you only use Docker) |

---

## 2. Run with Docker Compose (quickest)

```bash
docker compose up -d --build
```

| Service | URL |
|---|---|
| Frontend | http://localhost:3000 |
| API | http://localhost:8080 |
| WebSocket | ws://localhost:8080/ws/alerts |
| PostGIS | localhost:5433 (user `geo`, password `geo`, db `geofencing`) |

If a port is already used on your machine, override it, e.g. `API_PORT=8088 WEB_PORT=3001 docker compose up -d --build`
(the frontend image is built against `http://localhost:${API_PORT}` automatically).

Load demo data and start moving vehicles (needs Go):

```bash
cd backend
go run ./cmd/simulator -api http://localhost:8080
```

This creates four San Francisco geofences (one per category) with alert rules, registers three vehicles, and moves them every 2 seconds. Open the frontend and watch alerts arrive. Stop with Ctrl+C; re-running skips data that already exists (`-seed=false` to only drive).

Reset everything: `docker compose down -v`.

---

## 3. Local development (without containers for the app)

```bash
# 1. Database only
docker compose up -d db

# 2. API (PowerShell: $env:DATABASE_URL="..." ; go run ./cmd/api)
cd backend
DATABASE_URL="postgres://geo:geo@localhost:5433/geofencing?sslmode=disable" go run ./cmd/api

# 3. Frontend
cd frontend
cp .env.example .env.local      # set VITE_API_URL=http://localhost:8080
npm install
npm run dev                     # http://localhost:5173
```

Migrations are embedded in the binary and applied automatically at startup (guarded by a Postgres advisory lock).

### Backend configuration

| Variable | Default | Meaning |
|---|---|---|
| `DATABASE_URL` | (required) | Postgres URL. PostGIS extension is created by the first migration. |
| `PORT` | `8080` | Listen port |
| `CORS_ALLOWED_ORIGINS` | `*` | Comma-separated origins for CORS and the WebSocket origin check, e.g. `https://my-app.vercel.app` |
| `API_KEY` | (empty = auth off) | When set, every request needs `X-API-Key: <key>` (WebSocket: `?api_key=<key>`). `/healthz` stays open. |
| `RATE_LIMIT_RPS` / `RATE_LIMIT_BURST` | `5` / `10` | Per-vehicle token bucket on `POST /vehicles/location` (429 + `Retry-After` when exceeded). `RATE_LIMIT_RPS=0` disables it. |

### Frontend configuration

| Variable | Meaning |
|---|---|
| `VITE_API_URL` | API base URL. The WebSocket URL is derived from it (`https` → `wss`). |
| `VITE_API_KEY` | Only if the backend has `API_KEY` set. |

---

## 4. Tests

```bash
cd backend
go test ./...                                   # unit tests (domain rules)

# + end-to-end tests against real PostGIS (uses a separate geofencing_test DB created by compose)
TEST_DATABASE_URL="postgres://geo:geo@localhost:5433/geofencing_test?sslmode=disable" go test -race ./...
```

The end-to-end test drives the real HTTP API and a WebSocket client: validation errors, polygon self-intersection, entry/exit detection, boundary points, out-of-order (stale) pings, rule matching (`entry`/`exit`/`both`, specific vehicle vs all), WebSocket alert payloads, history filters/pagination, API key auth and rate limiting.

---

## 5. API testing guide (curl)

Every response includes `"time_ns"`: the handler's execution time in nanoseconds, as a string. Errors return
`{"error": "<code>", "field": "<field?>", "message": "<human readable>", "time_ns": "..."}` with 400 / 404 / 409 / 429 / 500.

```bash
API=http://localhost:8080
```

**Create a geofence**
```bash
curl -s -X POST $API/geofences -H 'Content-Type: application/json' -d '{
  "name": "Downtown Delivery Zone",
  "description": "Main delivery area for downtown customers",
  "coordinates": [[37.7749,-122.4194],[37.7849,-122.4194],[37.7849,-122.4094],[37.7749,-122.4094],[37.7749,-122.4194]],
  "category": "delivery_zone"
}'
# {"id":"geo_...","name":"Downtown Delivery Zone","status":"active",...,"time_ns":"..."}
```

**List geofences** (optional `category`, `limit`, `offset`)
```bash
curl -s "$API/geofences?category=delivery_zone"
```

**Register a vehicle**
```bash
curl -s -X POST $API/vehicles -H 'Content-Type: application/json' -d '{
  "vehicle_number": "KA-01-AB-1234", "driver_name": "John Doe", "vehicle_type": "truck", "phone": "+1234567890"
}'
```

**List vehicles**
```bash
curl -s $API/vehicles
```

**Configure an alert rule** (`vehicle_id` optional = all vehicles; `event_type`: `entry` | `exit` | `both`)
```bash
curl -s -X POST $API/alerts/configure -H 'Content-Type: application/json' -d '{
  "geofence_id": "geo_REPLACE", "vehicle_id": "veh_REPLACE", "event_type": "entry"
}'
```

**List alert rules** (optional `geofence_id`, `vehicle_id`)
```bash
curl -s "$API/alerts?geofence_id=geo_REPLACE"
```

**Watch real-time alerts** (in another terminal)
```bash
npx wscat -c ws://localhost:8080/ws/alerts      # or: websocat ws://localhost:8080/ws/alerts
```

**Send a location update** (inside the zone → entry event → alert pushed to WebSocket clients)
```bash
curl -s -X POST $API/vehicles/location -H 'Content-Type: application/json' -d '{
  "vehicle_id": "veh_REPLACE", "latitude": 37.78, "longitude": -122.415, "timestamp": "2025-01-15T10:35:00Z"
}'
# {"vehicle_id":"veh_...","location_updated":true,
#  "current_geofences":[{"geofence_id":"geo_...","geofence_name":"Downtown Delivery Zone","category":"delivery_zone","status":"inside"}],
#  "events":[{"event_id":"viol_...","event_type":"entry",...}],"alerts_triggered":1,"stale":false,"time_ns":"..."}
```

**Current location + geofences of a vehicle**
```bash
curl -s $API/vehicles/location/veh_REPLACE
```

**Violation history** (optional `vehicle_id`, `geofence_id`, `start_date`, `end_date`, `limit` (default 50, max 500), `offset`)
```bash
curl -s "$API/violations/history?vehicle_id=veh_REPLACE&start_date=2025-01-01T00:00:00Z&limit=100"
```

**Extras** (beyond the spec; spec endpoints are unchanged)
```bash
curl -s -X DELETE $API/alerts/alert_REPLACE   # turn off a rule (kept for audit, status=inactive)
curl -s "$API/alerts/recent?limit=20"         # last delivered alert payloads (frontend back-fill)
curl -s $API/healthz                          # DB ping + connected WebSocket clients
```

With `API_KEY` set, add `-H "X-API-Key: $API_KEY"` to each call.

---

## 6. Frontend usage guide

The map is on the left (top on mobile); the panels are on the right (bottom on mobile). The header badge shows the live WebSocket connection (it reconnects automatically with backoff) and the bell counts unread alerts.

- **Zones**: fill in name, category, description, then **Draw on map** (click corners, click the first corner to close) or paste coordinates. The list filters by category; the pin button flies the map to a zone.
- **Vehicles**: register a vehicle; click one to see its last position and the geofences it is inside right now.
- **Track**: pick a vehicle, click **Pick point on map** (or type lat/lng), optionally set a custom timestamp, and send. The result shows the geofences the vehicle is in and any entry/exit transitions it caused.
- **Rules**: create alert rules (geofence, optional vehicle, entry/exit/both), filter them, and turn them off.
- **Live**: real-time alert feed. Each alert also shows as a toast (restricted-zone entries in red and kept longer), and the vehicle marker pulses on the map.
- **History**: all entry/exit events, filterable by vehicle, geofence and date range, with pagination.

---

## 7. Architecture

```
React (Vite) ──REST──▶ Go API (net/http) ──pgx──▶ PostgreSQL + PostGIS
     ▲                       │
     └──── WebSocket ◀────── Hub goroutine (fan-out, per-client queues)
```

**Location update pipeline (`POST /vehicles/location`)**, one transaction:
1. `SELECT ... FROM vehicles WHERE id = $1 FOR UPDATE`: serialises concurrent pings for the same vehicle so a transition can't be emitted twice.
2. Insert into `vehicle_locations` (full movement history).
3. Point-in-polygon: `ST_Covers(geom, ST_MakePoint(lng, lat))` against active geofences, served by a GiST index. `ST_Covers` counts boundary points as inside.
4. If the ping is older than the last accepted one, stop here (stored, `stale: true`, no state change).
5. Diff the result with `vehicle_geofence_state` to get the entered and exited geofences (`domain.DiffGeofences`), update the state, and write each transition to `geofence_events` (served by `/violations/history`).
6. Match active alert rules (`domain.FirstMatchingRule`: same geofence, same vehicle or all, `entry`/`exit`/`both`; at most one alert per transition) and store each alert payload in `alerts`.
7. Commit, then `hub.Publish` each alert. Publishing is a non-blocking channel send, so the HTTP response never waits on WebSocket clients. Alerts are already in the database, so a slow or disconnected client loses nothing: the frontend back-fills from `/alerts/recent` after reconnecting.

**Coordinate order:** the API speaks `[latitude, longitude]` (as specified); PostGIS uses `(x=longitude, y=latitude)`. The swap happens in one place, `domain.PolygonWKT`, plus the `ST_MakePoint(lng, lat)` call, and both are covered by tests.

**Schema (main indexes)**: `geofences` (GiST on `geom`, btree on `category`) · `vehicles` (unique `vehicle_number`) · `vehicle_locations (vehicle_id, recorded_at DESC)` · `vehicle_geofence_state` PK `(vehicle_id, geofence_id)` · `alert_rules` (partial index on active rules per geofence; partial unique index blocks duplicate active rules) · `geofence_events` (`occurred_at`, `(vehicle_id, occurred_at)`, `(geofence_id, occurred_at)`) · `alerts (created_at)`.

---

## 8. Deployment

**Live deployment:** backend at `https://grtas.duckdns.org` (Go binary as a systemd service behind nginx + Let's Encrypt on AWS EC2, ap-south-1), database on Supabase (PostGIS), frontend on Vercel. Image: `rebel600/geofencing-api` on Docker Hub.

```bash
docker run -p 8080:8080 -e DATABASE_URL="postgres://..." rebel600/geofencing-api:latest
```

**Backend image**
```bash
docker build -t <dockerhub-user>/geofencing-api:latest ./backend
docker push <dockerhub-user>/geofencing-api:latest
```
Run it anywhere with `DATABASE_URL`, `CORS_ALLOWED_ORIGINS=https://<your-frontend>` and optionally `API_KEY`. The host must support WebSockets (a load balancer in front needs an idle timeout above 60s; the server pings every 50s).

**Supabase as the database:** enable the PostGIS extension (Database → Extensions → postgis), then use the *session pooler* or direct connection string (port 5432) with `?sslmode=require`. The transaction pooler (port 6543) also works: the API switches pgx to simple-protocol mode automatically.

**Frontend (Vercel):** project root `frontend`, framework Vite, env `VITE_API_URL=https://<your-api-host>`. The API must be served over HTTPS so the browser can open `wss://`.
