# Geofencing & Real-time Alert System

Track vehicles against polygonal geofences and get instant alerts when they enter or leave a zone.

- **Backend:** Go (stdlib `net/http`, `pgx`, `gorilla/websocket`) + PostgreSQL/PostGIS
- **Real-time:** WebSocket at `/ws/alerts`
- **Frontend:** React 19 + Vite + TypeScript + Tailwind + Leaflet (draw zones, click to move vehicles, live alert feed)

| | Link |
|---|---|
| Frontend | https://geofencing-realtime-alert-system-lilac.vercel.app |
| Backend API | https://grtas.duckdns.org (WebSocket: `wss://grtas.duckdns.org/ws/alerts`) |
| Docker image | [`rebel600/geofencing-api`](https://hub.docker.com/r/rebel600/geofencing-api) (`docker pull rebel600/geofencing-api:latest`) |
| Video walkthrough | _TBD_ |

**Setup, API guide (curl), frontend guide and architecture: see [SETUP.md](SETUP.md).**

```bash
docker compose up -d --build                         # db + api (:8080) + web (:3000)
cd backend && go run ./cmd/simulator -api http://localhost:8080   # demo data + moving vehicles
```

## Features

- All 9 required endpoints, exact paths, `time_ns` on every response (including errors)
- Point-in-polygon in PostGIS (`ST_Covers` + GiST index); polygon validation including self-intersection
- Entry/exit detection by diffing per-vehicle zone state inside a row-locked transaction; out-of-order pings don't create false transitions
- Rule-based alerts (`entry` / `exit` / `both`, one vehicle or all), persisted first, then broadcast asynchronously
- Bonus: per-vehicle rate limiting, optional API-key auth, pagination on all list endpoints, unit + end-to-end tests
- Demo simulator that seeds data and drives vehicles through the zones
