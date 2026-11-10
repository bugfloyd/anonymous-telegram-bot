package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func request(method, path string) Request {
	var req Request
	req.RawPath = path
	req.RequestContext.HTTP.Method = method
	return req
}

func TestServe(t *testing.T) {
	r := NewRouter()
	r.Handle(http.MethodGet, "/ok", func(context.Context, Request) Response {
		return JSON(http.StatusOK, map[string]bool{"ok": true})
	})

	tests := []struct {
		name, method, path string
		status             int
		body               string
	}{
		{"known route", http.MethodGet, "/ok", http.StatusOK, `{"ok":true}`},
		{"method is case-insensitive", "get", "/ok", http.StatusOK, `{"ok":true}`},
		{"unknown path", http.MethodGet, "/nope", http.StatusNotFound, `{"error":"not_found"}`},
		{"empty path is the root", http.MethodGet, "", http.StatusNotFound, `{"error":"not_found"}`},
		{"wrong method", http.MethodPost, "/ok", http.StatusMethodNotAllowed, `{"error":"method_not_allowed"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := r.Serve(context.Background(), request(tt.method, tt.path))
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != tt.status {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.status)
			}
			if resp.Body != tt.body {
				t.Errorf("body = %s, want %s", resp.Body, tt.body)
			}
		})
	}
}

func TestJSONHeaders(t *testing.T) {
	resp := JSON(http.StatusOK, struct{}{})
	for header, want := range map[string]string{
		"Content-Type":           "application/json",
		"Cache-Control":          "no-store",
		"X-Content-Type-Options": "nosniff",
	} {
		if got := resp.Headers[header]; got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

func TestJSONUnencodable(t *testing.T) {
	resp := JSON(http.StatusOK, map[string]any{"bad": make(chan int)})
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(resp.Body), &body); err != nil || body["error"] != "internal" {
		t.Fatalf("body = %s", resp.Body)
	}
}
