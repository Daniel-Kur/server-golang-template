// Package httpapi contains shared HTTP contracts and middleware.
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
)

type Error struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
	// Details must contain only intentionally public, serializable information.
	Details any `json:"details,omitempty"`
}

func (e *Error) Error() string { return e.Message }

type Envelope[T any] struct {
	Success bool `json:"success"`
	Data    T    `json:"data"`
}

func JSON(w http.ResponseWriter, status int, value any) {
	// Encode before committing headers so serialization failures remain JSON errors.
	body, err := json.Marshal(value)
	if err != nil {
		Failure(w, 500, "INTERNAL_SERVER_ERROR", "Internal Server Error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)+1))
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}
func Success[T any](w http.ResponseWriter, data T) {
	JSON(w, 200, Envelope[T]{Success: true, Data: data})
}

// WriteError maps expected operational errors, including wrapped errors. Unexpected
// errors always return a generic response; internal messages are never exposed.
func WriteError(w http.ResponseWriter, err error) {
	var public *Error
	if !errors.As(err, &public) || public == nil || public.Status < 400 || public.Status > 599 {
		public = &Error{Status: 500, Code: "INTERNAL_SERVER_ERROR", Message: "Internal Server Error"}
	}
	JSON(w, public.Status, struct {
		Success bool   `json:"success"`
		Error   *Error `json:"error"`
	}{Error: public})
}
func Failure(w http.ResponseWriter, status int, code, message string) {
	WriteError(w, &Error{Status: status, Code: code, Message: message})
}
