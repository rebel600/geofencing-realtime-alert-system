package httpapi

import (
	"bufio"
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Unwrap and Hijack let the WebSocket upgrade reach the underlying connection.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := r.ResponseWriter.(http.Hijacker); ok {
		r.status = http.StatusSwitchingProtocols
		return h.Hijack()
	}
	return nil, nil, errors.New("hijack not supported")
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.log.Info("request", "method", r.Method, "path", r.URL.Path, "status", rec.status,
			"duration", time.Since(start).String())
	})
}

func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		defer func() {
			if p := recover(); p != nil {
				if p == http.ErrAbortHandler {
					panic(p)
				}
				s.log.Error("panic", "path", r.URL.Path, "panic", p)
				writeError(w, start, http.StatusInternalServerError, "internal_error", "unexpected server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) originAllowed(origin string) bool {
	return slices.Contains(s.cfg.AllowedOrigins, "*") || slices.Contains(s.cfg.AllowedOrigins, strings.TrimRight(origin, "/"))
}

// CheckOrigin is used by the WebSocket upgrader. Non-browser clients send no Origin.
func CheckOrigin(allowed []string) func(*http.Request) bool {
	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		return origin == "" || slices.Contains(allowed, "*") || slices.Contains(allowed, strings.TrimRight(origin, "/"))
	}
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && s.originAllowed(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key")
			w.Header().Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireAPIKey enforces X-API-Key (or ?api_key= for WebSockets, since browsers
// can't set headers on a WebSocket handshake) when API_KEY is configured.
func (s *Server) requireAPIKey(next http.Handler) http.Handler {
	if s.cfg.APIKey == "" {
		return next
	}
	want := []byte(s.cfg.APIKey)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		got := r.Header.Get("X-API-Key")
		if got == "" {
			got = r.URL.Query().Get("api_key")
		}
		if subtle.ConstantTimeCompare([]byte(got), want) != 1 {
			writeError(w, time.Now(), http.StatusUnauthorized, "unauthorized", "missing or invalid API key (X-API-Key header)")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// vehicleLimiter is a per-vehicle token bucket for location updates.
type vehicleLimiter struct {
	mu       sync.Mutex
	limiters map[string]*limiterEntry
	rps      rate.Limit
	burst    int
}

type limiterEntry struct {
	lim      *rate.Limiter
	lastSeen time.Time
}

func newVehicleLimiter(rps float64, burst int) *vehicleLimiter {
	if rps <= 0 {
		return nil
	}
	if burst < 1 {
		burst = 1
	}
	return &vehicleLimiter{limiters: map[string]*limiterEntry{}, rps: rate.Limit(rps), burst: burst}
}

// Allow reports whether vehicleID may proceed. A nil limiter allows everything.
func (l *vehicleLimiter) Allow(vehicleID string) bool {
	if l == nil {
		return true
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.limiters[vehicleID]
	if !ok {
		// Opportunistic cleanup keeps memory bounded without a background goroutine.
		if len(l.limiters) > 10_000 {
			for id, old := range l.limiters {
				if now.Sub(old.lastSeen) > 10*time.Minute {
					delete(l.limiters, id)
				}
			}
		}
		e = &limiterEntry{lim: rate.NewLimiter(l.rps, l.burst)}
		l.limiters[vehicleID] = e
	}
	e.lastSeen = now
	return e.lim.AllowN(now, 1)
}
