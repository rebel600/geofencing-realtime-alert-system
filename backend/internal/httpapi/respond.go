package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/rebel600/geofencing-realtime-alert-system/backend/internal/domain"
)

// writeJSON serialises payload (which must encode to a JSON object) and
// appends "time_ns": the handler's execution time in nanoseconds, measured
// from start (taken at the top of the handler) to just before the write.
func writeJSON(w http.ResponseWriter, start time.Time, status int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil || len(body) < 2 || body[0] != '{' {
		body = []byte(`{"error":"internal error encoding response"}`)
		status = http.StatusInternalServerError
	}
	elapsed := strconv.FormatInt(time.Since(start).Nanoseconds(), 10)

	var buf bytes.Buffer
	buf.Grow(len(body) + 32)
	buf.Write(body[:len(body)-1]) // drop closing brace
	if len(body) > 2 {
		buf.WriteByte(',')
	}
	buf.WriteString(`"time_ns":"`)
	buf.WriteString(elapsed)
	buf.WriteString(`"}`)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

type errorBody struct {
	Error   string `json:"error"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, start time.Time, status int, code, message string) {
	writeJSON(w, start, status, errorBody{Error: code, Message: message})
}

func writeValidation(w http.ResponseWriter, start time.Time, err error) {
	var ve *domain.ValidationError
	if errors.As(err, &ve) {
		writeJSON(w, start, http.StatusBadRequest, errorBody{Error: "validation_error", Field: ve.Field, Message: ve.Error()})
		return
	}
	writeError(w, start, http.StatusBadRequest, "validation_error", err.Error())
}

const maxBodyBytes = 1 << 20 // 1 MiB

// decodeJSON strictly decodes a single JSON object from the request body.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var syntaxErr *json.SyntaxError
		var typeErr *json.UnmarshalTypeError
		var maxErr *http.MaxBytesError
		switch {
		case errors.Is(err, io.EOF):
			return &domain.ValidationError{Message: "request body is empty; expected a JSON object"}
		case errors.As(err, &syntaxErr), errors.Is(err, io.ErrUnexpectedEOF):
			return &domain.ValidationError{Message: "request body is not valid JSON"}
		case errors.As(err, &typeErr):
			return &domain.ValidationError{Field: typeErr.Field, Message: "has the wrong type, expected " + typeErr.Type.String()}
		case errors.As(err, &maxErr):
			return &domain.ValidationError{Message: "request body too large"}
		default:
			return &domain.ValidationError{Message: err.Error()}
		}
	}
	if dec.More() {
		return &domain.ValidationError{Message: "request body must contain a single JSON object"}
	}
	return nil
}

// parsePage reads optional ?limit=&offset= for list endpoints.
// defaultLimit 0 means "no limit unless asked".
func parsePage(r *http.Request, defaultLimit, maxLimit int) (limit, offset int, err error) {
	q := r.URL.Query()
	limit = defaultLimit
	if s := q.Get("limit"); s != "" {
		limit, err = strconv.Atoi(s)
		if err != nil || limit < 1 {
			return 0, 0, &domain.ValidationError{Field: "limit", Message: "must be a positive integer"}
		}
		if limit > maxLimit {
			limit = maxLimit
		}
	}
	if s := q.Get("offset"); s != "" {
		offset, err = strconv.Atoi(s)
		if err != nil || offset < 0 {
			return 0, 0, &domain.ValidationError{Field: "offset", Message: "must be a non-negative integer"}
		}
	}
	return limit, offset, nil
}
