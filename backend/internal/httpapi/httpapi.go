// Package httpapi routes Lambda function URL requests to handlers and writes
// JSON responses. It never logs requests: they carry IP addresses and other
// metadata the service must not keep.
package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
)

// MaxBody is the largest request body DecodeJSON accepts.
const MaxBody = 64 << 10

var (
	// ErrBadRequest is returned by DecodeJSON for any body it won't accept.
	ErrBadRequest = errors.New("httpapi: bad request")
	// ErrMediaType is returned by DecodeJSON for a body that isn't JSON.
	ErrMediaType = errors.New("httpapi: not application/json")
)

// DecodeJSON decodes the request body into v. It accepts exactly one JSON
// value of at most MaxBody bytes, with no unknown fields and nothing after it.
//
// The request must say it is application/json. Browsers send such requests
// across origins only after a CORS preflight, which the function URL answers
// for the app's own origin alone.
func DecodeJSON(req Request, v any) error {
	if !isJSON(req.Headers) {
		return ErrMediaType
	}
	body, err := Body(req)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return ErrBadRequest
	}
	if _, err := dec.Token(); err != io.EOF {
		return ErrBadRequest
	}
	return nil
}

// Body returns the raw request body, at most MaxBody bytes.
func Body(req Request) ([]byte, error) {
	body := []byte(req.Body)
	if req.IsBase64Encoded {
		decoded, err := base64.StdEncoding.DecodeString(req.Body)
		if err != nil {
			return nil, ErrBadRequest
		}
		body = decoded
	}
	if len(body) > MaxBody {
		return nil, ErrBadRequest
	}
	return body, nil
}

// DecodeError is the response for an error from DecodeJSON.
func DecodeError(err error) Response {
	if errors.Is(err, ErrMediaType) {
		return Error(http.StatusUnsupportedMediaType, "unsupported_media_type")
	}
	return Error(http.StatusBadRequest, "bad_request")
}

func isJSON(headers map[string]string) bool {
	for name, value := range headers {
		if strings.EqualFold(name, "content-type") {
			mediaType, _, _ := strings.Cut(value, ";")
			return strings.EqualFold(strings.TrimSpace(mediaType), "application/json")
		}
	}
	return false
}

// Request is a function URL request.
type Request = events.LambdaFunctionURLRequest

// Response is a function URL response.
type Response = events.LambdaFunctionURLResponse

// HandlerFunc handles one route.
type HandlerFunc func(ctx context.Context, req Request) Response

// Router dispatches by method and path. A pattern segment written {name}
// matches any one non-empty segment; handlers read it with Param. Patterns
// without parameters win over patterns with them.
type Router struct {
	routes []*route
	// Observe, if set, is told each handled request's route pattern (never
	// the path, which can hold IDs), status and how long it took.
	Observe func(route string, status int, elapsed time.Duration)
}

type route struct {
	pattern  string
	segments []string
	params   bool
	byMethod map[string]HandlerFunc
}

type paramsKey struct{}

// NewRouter returns an empty router.
func NewRouter() *Router {
	return &Router{}
}

// Handle registers a handler for method and pattern.
func (r *Router) Handle(method, pattern string, h HandlerFunc) {
	for _, rt := range r.routes {
		if rt.pattern == pattern {
			rt.byMethod[method] = h
			return
		}
	}
	segments := split(pattern)
	params := false
	for _, s := range segments {
		params = params || isParam(s)
	}
	r.routes = append(r.routes, &route{pattern: pattern, segments: segments, params: params, byMethod: map[string]HandlerFunc{method: h}})
	// Static patterns first.
	slices.SortStableFunc(r.routes, func(a, b *route) int {
		switch {
		case a.params == b.params:
			return 0
		case a.params:
			return 1
		default:
			return -1
		}
	})
}

// Serve is the Lambda entry point for a function URL.
func (r *Router) Serve(ctx context.Context, req Request) (Response, error) {
	segments := split(req.RawPath)
	method := strings.ToUpper(req.RequestContext.HTTP.Method)
	pathMatched := false
	for _, rt := range r.routes {
		params, ok := rt.match(segments)
		if !ok {
			continue
		}
		h, ok := rt.byMethod[method]
		if !ok {
			pathMatched = true
			continue
		}
		if params != nil {
			ctx = context.WithValue(ctx, paramsKey{}, params)
		}
		started := time.Now()
		resp := h(ctx, req)
		if r.Observe != nil {
			r.Observe(method+" "+rt.pattern, resp.StatusCode, time.Since(started))
		}
		return resp, nil
	}
	if pathMatched {
		return Error(http.StatusMethodNotAllowed, "method_not_allowed"), nil
	}
	return Error(http.StatusNotFound, "not_found"), nil
}

// Param returns a path parameter of the matched pattern ("" if none).
func Param(ctx context.Context, name string) string {
	params, _ := ctx.Value(paramsKey{}).(map[string]string)
	return params[name]
}

// Header returns a request header, whatever its case.
func Header(req Request, name string) string {
	for k, v := range req.Headers {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return ""
}

func (rt *route) match(segments []string) (map[string]string, bool) {
	if len(segments) != len(rt.segments) {
		return nil, false
	}
	var params map[string]string
	for i, s := range rt.segments {
		switch {
		case isParam(s):
			if segments[i] == "" {
				return nil, false
			}
			if params == nil {
				params = map[string]string{}
			}
			params[s[1:len(s)-1]] = segments[i]
		case s != segments[i]:
			return nil, false
		}
	}
	return params, true
}

func split(path string) []string {
	if path == "" || path == "/" {
		return []string{""}
	}
	return strings.Split(strings.TrimPrefix(path, "/"), "/")
}

func isParam(segment string) bool {
	return len(segment) > 2 && segment[0] == '{' && segment[len(segment)-1] == '}'
}

// JSON writes v as a JSON response with the given status.
func JSON(status int, v any) Response {
	body, err := json.Marshal(v)
	if err != nil {
		return Error(http.StatusInternalServerError, "internal")
	}
	return Response{
		StatusCode: status,
		Headers: map[string]string{
			"Content-Type":           "application/json",
			"Cache-Control":          "no-store",
			"X-Content-Type-Options": "nosniff",
		},
		Body: string(body),
	}
}

// Error writes a JSON error with a stable, machine-readable code.
func Error(status int, code string) Response {
	return JSON(status, map[string]string{"error": code})
}
