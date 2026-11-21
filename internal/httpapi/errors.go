// Copyright (C) 2026 The Nebla contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/nebla-cloud/nebla/internal/apierr"
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
