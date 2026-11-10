// Package httpapi routes Lambda function URL requests to handlers and writes
// JSON responses. It never logs requests: they carry IP addresses and other
// metadata the service must not keep.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/aws/aws-lambda-go/events"
)

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
