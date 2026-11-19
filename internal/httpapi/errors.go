package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Hexeption/nebla/internal/apierr"
)

// WriteError writes the standard error envelope.
func WriteError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": code, "message": msg},
	})
}

func (s *server) responseError(w http.ResponseWriter, _ *http.Request, err error) {
	var pe *apierr.E
	if errors.As(err, &pe) && pe != nil {
		WriteError(w, pe.Status, pe.Code, pe.Msg)
		return
	}
	var e apierr.E
	if errors.As(err, &e) {
		WriteError(w, e.Status, e.Code, e.Msg)
		return
	}
	s.log.Error("request failed", "err", err)
	WriteError(w, http.StatusInternalServerError, "internal_error", "internal error")
}

func requestError(w http.ResponseWriter, _ *http.Request, err error) {
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		WriteError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body too large")
		return
	}
	WriteError(w, http.StatusBadRequest, "invalid_input", err.Error())
}

func notFound(w http.ResponseWriter, _ *http.Request) {
	WriteError(w, http.StatusNotFound, "not_found", "not found")
}

func methodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
}
