// Package apierr holds the error type domain code returns to the HTTP layer.
package apierr

import "fmt"

type E struct {
	Status int
	Code   string
	Msg    string
}

func (e E) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Msg) }

func New(status int, code, msg string) E { return E{Status: status, Code: code, Msg: msg} }
