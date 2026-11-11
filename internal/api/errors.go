package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	. "github.com/edward-murrell/kowhai/internal/apitypes"
	"github.com/edward-murrell/kowhai/internal/service"
)

// WriteProblem writes an RFC 9457 problem document.
func WriteProblem(w http.ResponseWriter, status int, title, detail string) {
	p := Problem{Status: status, Title: title}
	if detail != "" {
		p.Detail = &detail
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(p)
}

// WriteError maps a service error to a problem response. Unknown errors
// are logged and reported as 500 without detail.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	var se *service.Error
	if errors.As(err, &se) {
		if se.Status == http.StatusUnauthorized {
			w.Header().Set("WWW-Authenticate", `Bearer realm="kowhai"`)
		}
		WriteProblem(w, se.Status, se.Title, se.Detail)
		return
	}
	slog.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	WriteProblem(w, http.StatusInternalServerError, "Internal error", "")
}

// requestError handles bad input caught by the generated decoder.
func requestError(w http.ResponseWriter, r *http.Request, err error) {
	WriteProblem(w, http.StatusBadRequest, "Bad request", err.Error())
}
