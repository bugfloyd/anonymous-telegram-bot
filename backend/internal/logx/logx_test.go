package logx

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"strings"
	"testing"
)

func lines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("not JSON: %q", line)
		}
		out = append(out, m)
	}
	return out
}

func TestDropsEverythingNotAllowed(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, "api")
	log.Warn("request failed",
		slog.String(KeyRoute, "POST /session"),
		slog.Int(KeyStatus, 500),
		ErrorKind(&fs.PathError{Op: "open", Path: "/secret/account-123", Err: errors.New("x")}),
		slog.String("account_id", "AAAAAAAAAAAAAAAAAAAAAA"),
		slog.String("ip", "203.0.113.7"),
		slog.Group("request", slog.String(KeyRoute, "hidden in a group")),
	)
	log.WithGroup("g").Warn("grouped", slog.String(KeyRoute, "x"))

	got := lines(t, &buf)
	if len(got) != 2 {
		t.Fatalf("got %d lines: %s", len(got), buf.String())
	}
	first := got[0]
	for key, want := range map[string]any{
		"msg": "request failed", "fn": "api", "route": "POST /session",
		"status": float64(500), "error_kind": "*fs.PathError",
	} {
		if first[key] != want {
			t.Errorf("%s = %v, want %v", key, first[key], want)
		}
	}
	for _, key := range []string{"account_id", "ip", "request"} {
		if _, ok := first[key]; ok {
			t.Errorf("%s was logged", key)
		}
	}
	if strings.Contains(buf.String(), "account-123") || strings.Contains(buf.String(), "203.0.113.7") {
		t.Errorf("an identifier leaked: %s", buf.String())
	}
	if _, ok := got[1]["g"]; ok {
		t.Errorf("a group was logged: %v", got[1])
	}
}

func TestLevelIsWarning(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, "api")
	log.Info("routine")
	log.Debug("noise")
	if buf.Len() != 0 {
		t.Fatalf("logged below warning: %s", buf.String())
	}
	log.Error("bad")
	if len(lines(t, &buf)) != 1 {
		t.Fatal("error not logged")
	}
}

func TestLongValuesAreCut(t *testing.T) {
	var buf bytes.Buffer
	New(&buf, "api").Warn("x", slog.String(KeyRoute, strings.Repeat("r", 500)))
	if got := lines(t, &buf)[0]["route"].(string); len(got) != 100 {
		t.Fatalf("route length = %d", len(got))
	}
}

func TestErrorKindNil(t *testing.T) {
	var buf bytes.Buffer
	New(&buf, "api").Warn("x", ErrorKind(nil))
	if _, ok := lines(t, &buf)[0]["error_kind"]; ok {
		t.Fatal("nil error logged")
	}
}
