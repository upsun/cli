package auth

import (
	"context"
	"fmt"
	"net/http"
)

// eventCtxKey is the context key for storing the event name.
type eventCtxKey struct{}

// interactiveCtxKey is the context key for storing the interactive mode flag.
type interactiveCtxKey struct{}

// WithEventName returns a new context that carries the provided event name.
func WithEventName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, eventCtxKey{}, name)
}

// EventNameFromContext retrieves an event name previously stored with WithEventName.
// It returns an empty string if none is set.
func EventNameFromContext(ctx context.Context) string {
	v, _ := ctx.Value(eventCtxKey{}).(string)
	return v
}

// WithInteractive returns a new context that carries the interactive mode flag.
func WithInteractive(ctx context.Context, interactive bool) context.Context {
	return context.WithValue(ctx, interactiveCtxKey{}, interactive)
}

// InteractiveFromContext retrieves the interactive flag previously stored with WithInteractive.
// It returns true (the default) if none is set.
func InteractiveFromContext(ctx context.Context) bool {
	v, ok := ctx.Value(interactiveCtxKey{}).(bool)
	if !ok {
		return true // default to interactive
	}
	return v
}

// EventTransport wraps an http.RoundTripper to add event tracking headers.
type EventTransport struct {
	// Base is the underlying RoundTripper to use for requests.
	Base http.RoundTripper

	// EventName is the command name for the X-CLI-Event header.
	// If empty, no header is added.
	EventName string

	// Interactive indicates whether the CLI is running in interactive mode.
	Interactive bool

	// UserAgent is the User-Agent string to send.
	// If empty, or a User-Agent is already set on the request, no header is added.
	UserAgent string
}

// RoundTrip adds the X-CLI-Event and User-Agent headers to the request.
func (t *EventTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.EventName != "" {
		// Format: command=<name>; interactive=<bool>
		headerValue := fmt.Sprintf("command=%s; interactive=%t", t.EventName, t.Interactive)
		req.Header.Set("X-CLI-Event", headerValue)
	}
	if t.UserAgent != "" && req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", t.UserAgent)
	}
	return t.Base.RoundTrip(req)
}
