// Package server assembles the control plane's HTTP surface: the generated
// /api/v1 routes, and the Server-Sent Events stream beside them. Streaming
// does not fit the generated shape (M2), so the events handler is the one
// hand-written route (docs/2-architecture.md § Control plane).
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/briolink/briolink/control/events"
	api "github.com/briolink/briolink/gen/http"
)

// Role is the role the control plane's routes are made for (the spec's one
// role). A token acting as another role is let through authentication and
// refused with a 403.
const Role = "operator"

// AdminRole is the role of the routes that change credentials (put, rotate
// and delete a secret, issue and revoke a token). It is held apart from Role
// so that a token an automated runner holds cannot mint tokens or touch
// secrets; it reaches no other route.
const AdminRole = "admin"

// Roles are the roles a token may act as.
var Roles = []string{Role, AdminRole}

// Tokens checks a presented API token: the role it acts as, or an error when
// it is unknown, expired, revoked or its use could not be recorded. use is
// "METHOD /path", for the audit log. *vault.Vault is one.
type Tokens interface {
	Authenticate(ctx context.Context, token, use string) (role, name string, err error)
}

// actorKey holds, in a request's context, the name of the token that made it.
type actorKey struct{}

type caller struct{ name string }

// Actor is how the audit log names the token that made the request whose
// context this is ("token:<name>", as the authentication's own lines do), or
// "" when no token was accepted. A route that writes names this actor.
func Actor(ctx context.Context) string {
	if c, ok := ctx.Value(actorKey{}).(*caller); ok && c.name != "" {
		return "token:" + c.name
	}
	return ""
}

// WithActor returns ctx as the request of the named token would carry it,
// for a test of what is below the handler.
func WithActor(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, actorKey{}, &caller{name})
}

// withCaller gives each request a place for authentication to record who made it.
func withCaller(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), actorKey{}, &caller{})))
	})
}

// Config is what the handler needs.
type Config struct {
	Service api.Service
	Events  *events.Broker
	Tokens  Tokens
	// Heartbeat is how often an idle event stream writes a comment line, so a
	// dead connection is noticed and proxies keep it open. Zero is 15 s.
	Heartbeat time.Duration
}

// Handler serves /api/v1 and /api/v1/events.
func Handler(c Config) http.Handler {
	if c.Heartbeat <= 0 {
		c.Heartbeat = 15 * time.Second
	}
	auth := BearerAuth(c.Tokens)
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/events", noStore(authorised(auth, http.HandlerFunc(c.stream))))
	mux.Handle("/", noStore(withCaller(api.Handler(c.Service, auth))))
	return mux
}

// BearerAuth authenticates a request carrying "Authorization: Bearer <token>"
// as the role the token acts as. Each use is recorded by tokens under
// "METHOD /path" (the query is left out: it is the caller's, not ours).
func BearerAuth(tokens Tokens) api.Auth {
	return func(r *http.Request) (string, bool) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || got == "" {
			return "", false
		}
		role, name, err := tokens.Authenticate(r.Context(), got, r.Method+" "+r.URL.Path)
		if err != nil {
			return "", false
		}
		if c, ok := r.Context().Value(actorKey{}).(*caller); ok {
			c.name = name
		}
		return role, true
	}
}

// noStore keeps every answer out of caches: nothing here is public (A8).
func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func authorised(auth api.Auth, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role, ok := auth(r)
		switch {
		case !ok:
			problem(w, http.StatusUnauthorized, "authentication is required")
		case role != Role:
			problem(w, http.StatusForbidden, "this role may not use this route")
		default:
			next.ServeHTTP(w, r)
		}
	})
}

func problem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(api.Problem{Type: "about:blank", Title: http.StatusText(status), Status: status, Detail: detail})
}

// stream writes the events as text/event-stream: first, if the client sent a
// Last-Event-ID, those after it (the broker keeps the last few), then each
// new one, with a comment line when idle. It ends when the client goes, or
// when the client fell so far behind that the broker cut it off; the client
// then reconnects with the last ID it saw.
func (c Config) stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		problem(w, http.StatusInternalServerError, "")
		return
	}
	after := int64(-1) // a client that does not ask to resume gets only what comes next
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			problem(w, http.StatusBadRequest, "Last-Event-ID must be an event number")
			return
		}
		after = n
	}
	missed, live, stop := c.Events.Subscribe(after)
	defer stop()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ": connected\n\n")
	for _, e := range missed {
		write(w, e)
	}
	flusher.Flush()

	tick := time.NewTicker(c.Heartbeat)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case e, open := <-live:
			if !open {
				return
			}
			write(w, e)
		case <-tick.C:
			fmt.Fprint(w, ": ping\n\n")
		}
		flusher.Flush()
	}
}

func write(w http.ResponseWriter, e events.Event) {
	fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.ID, e.Type, e.Data)
}
