// Package server assembles the control plane's HTTP surface: the generated
// /api/v1 routes, and the Server-Sent Events stream beside them. Streaming
// does not fit the generated shape (M2), so the events handler is the one
// hand-written route (docs/2-architecture.md § Control plane).
package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/briolink/briolink/control/events"
	api "github.com/briolink/briolink/gen/http"
)

// Role is the one role the control plane's routes know; the secret store
// (#58) brings real accounts.
const Role = "operator"

// Config is what the handler needs.
type Config struct {
	Service api.Service
	Events  *events.Broker
	// Token is the bearer token that authenticates as Role. Empty means no
	// request authenticates.
	Token string
	// Heartbeat is how often an idle event stream writes a comment line, so a
	// dead connection is noticed and proxies keep it open. Zero is 15 s.
	Heartbeat time.Duration
}

// Handler serves /api/v1 and /api/v1/events.
func Handler(c Config) http.Handler {
	if c.Heartbeat <= 0 {
		c.Heartbeat = 15 * time.Second
	}
	auth := BearerAuth(c.Token)
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/events", noStore(authorised(auth, http.HandlerFunc(c.stream))))
	mux.Handle("/", noStore(api.Handler(c.Service, auth)))
	return mux
}

// BearerAuth authenticates a request carrying "Authorization: Bearer <token>"
// as Role. The comparison takes the same time whatever the token's length.
func BearerAuth(token string) api.Auth {
	want := sha256.Sum256([]byte(token))
	return func(r *http.Request) (string, bool) {
		if token == "" {
			return "", false
		}
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			return "", false
		}
		sum := sha256.Sum256([]byte(got))
		if subtle.ConstantTimeCompare(sum[:], want[:]) != 1 {
			return "", false
		}
		return Role, true
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
