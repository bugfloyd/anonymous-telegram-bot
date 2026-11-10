// Package logx is the only way the backend logs.
//
// It writes JSON at warning level and above, and drops every attribute that
// isn't on a short allow-list (and every group), so identifiers, IP addresses
// and request data can't end up in the logs by mistake. Messages must be
// constant strings: never format values into them.
package logx

import (
	"fmt"
	"io"
	"log/slog"
)

// Allowed attribute keys. Values must never identify a person, device,
// account, link, mailbox or request.
const (
	KeyFunction  = "fn"          // which Lambda function
	KeyRoute     = "route"       // route pattern, e.g. "POST /session"
	KeyStatus    = "status"      // HTTP status returned
	KeyErrorKind = "error_kind"  // Go type of an error, never its message
	KeyDuration  = "duration_ms" // how long something took
	KeyCount     = "count"       // how many items were involved
)

var allowed = map[string]bool{
	KeyFunction: true, KeyRoute: true, KeyStatus: true,
	KeyErrorKind: true, KeyDuration: true, KeyCount: true,
}

// New returns a logger for one function writing to w.
func New(w io.Writer, function string) *slog.Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: slog.LevelWarn,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) > 0 {
				return slog.Attr{}
			}
			switch a.Key {
			case slog.TimeKey, slog.LevelKey, slog.MessageKey:
				return a
			}
			if !allowed[a.Key] {
				return slog.Attr{}
			}
			if a.Value.Kind() == slog.KindString && len(a.Value.String()) > 100 {
				a.Value = slog.StringValue(a.Value.String()[:100])
			}
			return a
		},
	})
	return slog.New(h).With(slog.String(KeyFunction, function))
}

// ErrorKind describes an error by its Go type only. Error messages can carry
// keys or values from the request, so they are never logged.
func ErrorKind(err error) slog.Attr {
	if err == nil {
		return slog.Attr{}
	}
	return slog.String(KeyErrorKind, fmt.Sprintf("%T", err))
}
