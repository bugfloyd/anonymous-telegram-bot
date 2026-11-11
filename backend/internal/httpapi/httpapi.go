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
	"strings"

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
	body := []byte(req.Body)
	if req.IsBase64Encoded {
		decoded, err := base64.StdEncoding.DecodeString(req.Body)
		if err != nil {
			return ErrBadRequest
		}
		body = decoded
	}
	if len(body) > MaxBody {
		return ErrBadRequest
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

// Router dispatches by method and exact path.
type Router struct {
	routes map[string]map[string]HandlerFunc // path -> method -> handler
}

// NewRouter returns an empty router.
func NewRouter() *Router {
	return &Router{routes: map[string]map[string]HandlerFunc{}}
}

// Handle registers a handler for method and path.
func (r *Router) Handle(method, path string, h HandlerFunc) {
	if r.routes[path] == nil {
		r.routes[path] = map[string]HandlerFunc{}
	}
	r.routes[path][method] = h
}

// Serve is the Lambda entry point for a function URL.
func (r *Router) Serve(ctx context.Context, req Request) (Response, error) {
	path := req.RawPath
	if path == "" {
		path = "/"
	}
	byMethod, ok := r.routes[path]
	if !ok {
		return Error(http.StatusNotFound, "not_found"), nil
	}
	h, ok := byMethod[strings.ToUpper(req.RequestContext.HTTP.Method)]
	if !ok {
		return Error(http.StatusMethodNotAllowed, "method_not_allowed"), nil
	}
	return h(ctx, req), nil
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
