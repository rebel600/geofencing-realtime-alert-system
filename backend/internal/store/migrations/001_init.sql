CREATE EXTENSION IF NOT EXISTS postgis;

CREATE TABLE geofences (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    category    TEXT NOT NULL CHECK (category IN ('delivery_zone', 'restricted_zone', 'toll_zone', 'customer_area')),
    -- Original [lat, lng] ring exactly as submitted, returned verbatim by the API.
    coordinates JSONB NOT NULL,
    -- Same ring as a PostGIS polygon (x = lng, y = lat) used for spatial queries.
    geom        geometry(Polygon, 4326) NOT NULL,
    status      TEXT NOT NULL DEFAULT 'active',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX geofences_geom_gist ON geofences USING GIST (geom);
CREATE INDEX geofences_category_idx ON geofences (category);

CREATE TABLE vehicles (
    id               TEXT PRIMARY KEY,
    vehicle_number   TEXT NOT NULL UNIQUE,
    driver_name      TEXT NOT NULL,
    vehicle_type     TEXT NOT NULL,
    phone            TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'active',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Latest accepted position (denormalised for O(1) lookups).
    last_latitude    DOUBLE PRECISION,
    last_longitude   DOUBLE PRECISION,
    last_recorded_at TIMESTAMPTZ
);

-- Full movement history: every ping, including out-of-order ones.
CREATE TABLE vehicle_locations (
    id          BIGSERIAL PRIMARY KEY,
    vehicle_id  TEXT NOT NULL REFERENCES vehicles (id) ON DELETE CASCADE,
    latitude    DOUBLE PRECISION NOT NULL,
    longitude   DOUBLE PRECISION NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX vehicle_locations_vehicle_time_idx ON vehicle_locations (vehicle_id, recorded_at DESC);

-- Which geofences each vehicle is currently inside; diffed on every update
-- to detect entry/exit transitions.
CREATE TABLE vehicle_geofence_state (
    vehicle_id  TEXT NOT NULL REFERENCES vehicles (id) ON DELETE CASCADE,
    geofence_id TEXT NOT NULL REFERENCES geofences (id) ON DELETE CASCADE,
    entered_at  TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (vehicle_id, geofence_id)
);

CREATE TABLE alert_rules (
    id          TEXT PRIMARY KEY,
    geofence_id TEXT NOT NULL REFERENCES geofences (id) ON DELETE CASCADE,
    vehicle_id  TEXT REFERENCES vehicles (id) ON DELETE CASCADE, -- NULL = all vehicles
    event_type  TEXT NOT NULL CHECK (event_type IN ('entry', 'exit', 'both')),
    status      TEXT NOT NULL DEFAULT 'active',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX alert_rules_geofence_idx ON alert_rules (geofence_id) WHERE status = 'active';
-- No two identical active rules.
CREATE UNIQUE INDEX alert_rules_unique_active
    ON alert_rules (geofence_id, COALESCE(vehicle_id, ''), event_type) WHERE status = 'active';

-- Every entry/exit transition (served by GET /violations/history).
CREATE TABLE geofence_events (
    id          TEXT PRIMARY KEY,
    vehicle_id  TEXT NOT NULL REFERENCES vehicles (id) ON DELETE CASCADE,
    geofence_id TEXT NOT NULL REFERENCES geofences (id) ON DELETE CASCADE,
    event_type  TEXT NOT NULL CHECK (event_type IN ('entry', 'exit')),
    latitude    DOUBLE PRECISION NOT NULL,
    longitude   DOUBLE PRECISION NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX geofence_events_time_idx ON geofence_events (occurred_at DESC);
CREATE INDEX geofence_events_vehicle_time_idx ON geofence_events (vehicle_id, occurred_at DESC);
CREATE INDEX geofence_events_geofence_time_idx ON geofence_events (geofence_id, occurred_at DESC);

-- Alerts actually delivered (events that matched a rule), with the exact payload sent.
CREATE TABLE alerts (
    id          TEXT PRIMARY KEY,           -- evt_...
    event_id    TEXT NOT NULL REFERENCES geofence_events (id) ON DELETE CASCADE,
    rule_id     TEXT REFERENCES alert_rules (id) ON DELETE SET NULL,
    payload     JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX alerts_created_idx ON alerts (created_at DESC);
