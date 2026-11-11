package domain

import (
	"reflect"
	"strings"
	"testing"
)

func square() [][]float64 {
	return [][]float64{
		{37.7749, -122.4194},
		{37.7849, -122.4194},
		{37.7849, -122.4094},
		{37.7749, -122.4094},
		{37.7749, -122.4194},
	}
}

func TestValidatePolygon(t *testing.T) {
	tests := []struct {
		name    string
		coords  [][]float64
		wantErr string
	}{
		{"valid square", square(), ""},
		{"valid triangle", [][]float64{{0, 0}, {1, 0}, {0, 1}, {0, 0}}, ""},
		{"too few points", [][]float64{{0, 0}, {1, 0}, {0, 0}}, "at least 4 points"},
		{"not closed", [][]float64{{0, 0}, {1, 0}, {1, 1}, {0, 1}}, "must be closed"},
		{"bad pair length", [][]float64{{0, 0}, {1}, {1, 1}, {0, 0}}, "point 1"},
		{"latitude out of range", [][]float64{{0, 0}, {91, 0}, {1, 1}, {0, 0}}, "latitude"},
		{"longitude out of range", [][]float64{{0, 0}, {1, 181}, {1, 1}, {0, 0}}, "longitude"},
		{"duplicate points only", [][]float64{{0, 0}, {1, 1}, {1, 1}, {0, 0}}, "3 unique"},
		{"boundary values ok", [][]float64{{-90, -180}, {90, -180}, {90, 180}, {-90, -180}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePolygon(tt.coords)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
			}
			if !IsValidation(err) {
				t.Fatalf("want ValidationError, got %T", err)
			}
		})
	}
}

func TestPolygonWKTSwapsToLngLat(t *testing.T) {
	got := PolygonWKT([][]float64{{10, 20}, {11, 20}, {11, 21}, {10, 20}})
	want := "POLYGON((20 10,20 11,21 11,20 10))"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestParseTimestamp(t *testing.T) {
	ts, err := ParseTimestamp("timestamp", "2025-01-15T12:35:00+02:00")
	if err != nil {
		t.Fatal(err)
	}
	if got := ts.Format("2006-01-02T15:04:05Z07:00"); got != "2025-01-15T10:35:00Z" {
		t.Fatalf("want UTC normalisation, got %s", got)
	}
	for _, bad := range []string{"", "yesterday", "2025-01-15"} {
		if _, err := ParseTimestamp("timestamp", bad); !IsValidation(err) {
			t.Errorf("%q: want validation error, got %v", bad, err)
		}
	}
}

func TestDiffGeofences(t *testing.T) {
	tests := []struct {
		name                  string
		prev, curr            []string
		wantEntered, wantExit []string
	}{
		{"first ping inside two zones", nil, []string{"b", "a"}, []string{"a", "b"}, nil},
		{"no change", []string{"a"}, []string{"a"}, nil, nil},
		{"moved from a to b", []string{"a"}, []string{"b"}, []string{"b"}, []string{"a"}},
		{"left everything", []string{"a", "c"}, nil, nil, []string{"a", "c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entered, exited := DiffGeofences(tt.prev, tt.curr)
			if !reflect.DeepEqual(entered, tt.wantEntered) || !reflect.DeepEqual(exited, tt.wantExit) {
				t.Fatalf("got entered=%v exited=%v, want entered=%v exited=%v", entered, exited, tt.wantEntered, tt.wantExit)
			}
		})
	}
}

func TestRuleMatching(t *testing.T) {
	v1 := "veh_1"
	rules := []AlertRule{
		{ID: "r_specific_exit", GeofenceID: "geo_1", VehicleID: &v1, EventType: RuleExit},
		{ID: "r_all_both", GeofenceID: "geo_2", EventType: RuleBoth},
		{ID: "r_all_entry", GeofenceID: "geo_1", EventType: RuleEntry},
	}
	tests := []struct {
		vehicle, geofence string
		event             EventType
		wantRule          string
	}{
		{"veh_1", "geo_1", EventExit, "r_specific_exit"},
		{"veh_2", "geo_1", EventExit, ""}, // specific rule is for veh_1 only
		{"veh_2", "geo_1", EventEntry, "r_all_entry"},
		{"veh_9", "geo_2", EventEntry, "r_all_both"},
		{"veh_9", "geo_2", EventExit, "r_all_both"},
		{"veh_1", "geo_3", EventEntry, ""}, // no rule for this geofence
	}
	for _, tt := range tests {
		r, ok := FirstMatchingRule(rules, tt.vehicle, tt.geofence, tt.event)
		if tt.wantRule == "" {
			if ok {
				t.Errorf("%s/%s/%s: want no match, got %s", tt.vehicle, tt.geofence, tt.event, r.ID)
			}
			continue
		}
		if !ok || r.ID != tt.wantRule {
			t.Errorf("%s/%s/%s: want %s, got %q (ok=%v)", tt.vehicle, tt.geofence, tt.event, tt.wantRule, r.ID, ok)
		}
	}
}

func TestNewID(t *testing.T) {
	id := NewID("geo")
	if !strings.HasPrefix(id, "geo_") || len(id) != len("geo_")+12 {
		t.Fatalf("unexpected id %q", id)
	}
	if NewID("geo") == id {
		t.Fatal("ids should be random")
	}
}

func TestCategoryAndRuleEventValidity(t *testing.T) {
	if !CategoryRestricted.Valid() || Category("parking").Valid() {
		t.Fatal("category validation wrong")
	}
	if !RuleBoth.Valid() || RuleEventType("enter").Valid() {
		t.Fatal("rule event validation wrong")
	}
}
