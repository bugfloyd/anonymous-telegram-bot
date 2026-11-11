package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
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

func TestServeParams(t *testing.T) {
	r := NewRouter()
	echo := func(name string) HandlerFunc {
		return func(ctx context.Context, _ Request) Response {
			return JSON(http.StatusOK, map[string]string{"route": name, "id": Param(ctx, "id")})
		}
	}
	r.Handle(http.MethodGet, "/links/{id}", echo("get"))
	r.Handle(http.MethodDelete, "/links/{id}", echo("delete"))
	r.Handle(http.MethodPut, "/links/{id}/bundle", echo("bundle"))
	r.Handle(http.MethodGet, "/links/mine", echo("mine"))

	tests := []struct {
		method, path string
		status       int
		body         string
	}{
		{http.MethodGet, "/links/abc", http.StatusOK, `{"id":"abc","route":"get"}`},
		{http.MethodDelete, "/links/abc", http.StatusOK, `{"id":"abc","route":"delete"}`},
		{http.MethodPut, "/links/abc/bundle", http.StatusOK, `{"id":"abc","route":"bundle"}`},
		{http.MethodGet, "/links/mine", http.StatusOK, `{"id":"","route":"mine"}`}, // static wins
		{http.MethodPost, "/links/abc", http.StatusMethodNotAllowed, `{"error":"method_not_allowed"}`},
		{http.MethodGet, "/links/", http.StatusNotFound, `{"error":"not_found"}`},
		{http.MethodGet, "/links", http.StatusNotFound, `{"error":"not_found"}`},
		{http.MethodGet, "/links/abc/extra", http.StatusNotFound, `{"error":"not_found"}`},
	}
	for _, tt := range tests {
		resp, _ := r.Serve(context.Background(), request(tt.method, tt.path))
		if resp.StatusCode != tt.status || resp.Body != tt.body {
			t.Errorf("%s %s = %d %s, want %d %s", tt.method, tt.path, resp.StatusCode, resp.Body, tt.status, tt.body)
		}
	}
}

func TestObserve(t *testing.T) {
	r := NewRouter()
	r.Handle(http.MethodGet, "/links/{id}", func(context.Context, Request) Response { return JSON(http.StatusOK, struct{}{}) })
	var seen []string
	r.Observe = func(route string, status int, elapsed time.Duration) {
		seen = append(seen, fmt.Sprintf("%s %d %v", route, status, elapsed >= 0))
	}
	_, _ = r.Serve(context.Background(), request(http.MethodGet, "/links/secret-id"))
	_, _ = r.Serve(context.Background(), request(http.MethodGet, "/nope"))
	// The pattern, not the path: no IDs. Unrouted requests aren't observed.
	if len(seen) != 1 || seen[0] != "GET /links/{id} 200 true" {
		t.Fatalf("observed %v", seen)
	}
}

func TestHeader(t *testing.T) {
	req := Request{Headers: map[string]string{"authorization": "Bearer x"}}
	if Header(req, "Authorization") != "Bearer x" || Header(req, "X-Missing") != "" {
		t.Fatal("Header lookup failed")
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

var jsonHeaders = map[string]string{"content-type": "application/json"}

func jsonRequest(body string) Request {
	return Request{Headers: jsonHeaders, Body: body}
}

func TestDecodeJSON(t *testing.T) {
	type body struct {
		Name string `json:"name"`
	}
	tests := []struct {
		name   string
		req    Request
		ok     bool
		result string
	}{
		{"plain", jsonRequest(`{"name":"a"}`), true, "a"},
		{"base64", Request{Headers: jsonHeaders, Body: "eyJuYW1lIjoiYiJ9", IsBase64Encoded: true}, true, "b"},
		{"whitespace after", jsonRequest("{\"name\":\"c\"}\n "), true, "c"},
		{"charset parameter", Request{Headers: map[string]string{"Content-Type": "Application/JSON; charset=utf-8"}, Body: `{"name":"d"}`}, true, "d"},
		{"unknown field", jsonRequest(`{"name":"a","x":1}`), false, ""},
		{"two values", jsonRequest(`{"name":"a"}{"name":"b"}`), false, ""},
		{"trailing garbage", jsonRequest(`{"name":"a"} x`), false, ""},
		{"empty", jsonRequest(""), false, ""},
		{"bad base64", Request{Headers: jsonHeaders, Body: "%%%", IsBase64Encoded: true}, false, ""},
		{"too large", jsonRequest(`{"name":"` + strings.Repeat("a", MaxBody) + `"}`), false, ""},
		{"wrong type", jsonRequest(`{"name":5}`), false, ""},
		{"no content type", Request{Body: `{"name":"a"}`}, false, ""},
		{"text/plain", Request{Headers: map[string]string{"content-type": "text/plain"}, Body: `{"name":"a"}`}, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got body
			err := DecodeJSON(tt.req, &got)
			if tt.ok && (err != nil || got.Name != tt.result) {
				t.Fatalf("got %+v, %v", got, err)
			}
			if !tt.ok && err == nil {
				t.Fatalf("accepted: %+v", got)
			}
		})
	}
}

func TestDecodeError(t *testing.T) {
	if got := DecodeError(DecodeJSON(Request{Body: "{}"}, &struct{}{})).StatusCode; got != http.StatusUnsupportedMediaType {
		t.Errorf("no content type: status %d", got)
	}
	if got := DecodeError(DecodeJSON(jsonRequest("x"), &struct{}{})).StatusCode; got != http.StatusBadRequest {
		t.Errorf("bad body: status %d", got)
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
