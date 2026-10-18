// Package events is the control plane's in-process event stream: a broker
// that numbers events, keeps the last few for reconnecting clients, and fans
// them out to subscribers (docs/2-architecture.md § Control plane). Events
// are not the record of anything; the database is. A client that misses some
// rereads the API.
package events

import (
	"encoding/json"
	"sync"
)

// Event is one thing that happened, numbered in publication order from 1.
type Event struct {
	ID   int64           `json:"id"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// Retained is how many past events a new subscriber can be given.
const Retained = 256

// buffer is how many events a subscriber may fall behind before it is cut off.
const buffer = 64

// Broker numbers and delivers events. The zero value is not ready; use New.
type Broker struct {
	mu   sync.Mutex
	last int64
	past []Event // the last Retained events, oldest first
	subs map[*sub]struct{}
}

type sub struct{ ch chan Event }

// New returns an empty broker.
func New() *Broker { return &Broker{subs: map[*sub]struct{}{}} }

// Publish numbers an event whose data is v marshalled as JSON, keeps it and
// hands it to every subscriber. A subscriber that is full is closed instead
// of waited for; it reconnects with the last ID it saw. A value that cannot
// be marshalled is a programming error and panics.
func (b *Broker) Publish(typ string, v any) Event {
	data, err := json.Marshal(v)
	if err != nil {
		panic("events: " + err.Error())
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.last++
	e := Event{ID: b.last, Type: typ, Data: data}
	if len(b.past) == Retained {
		b.past = append(b.past[:0], b.past[1:]...)
	}
	b.past = append(b.past, e)
	for s := range b.subs {
		select {
		case s.ch <- e:
		default:
			delete(b.subs, s)
			close(s.ch)
		}
	}
	return e
}

// Subscribe returns the retained events after ID after, in order (none when
// after is negative), and a channel with every event published from now on. The channel is closed when
// stop is called or when the subscriber fell too far behind. Nothing is
// missed between the two: both are taken under one lock.
func (b *Broker) Subscribe(after int64) (missed []Event, live <-chan Event, stop func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, e := range b.past {
		if after >= 0 && e.ID > after {
			missed = append(missed, e)
		}
	}
	s := &sub{ch: make(chan Event, buffer)}
	b.subs[s] = struct{}{}
	return missed, s.ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if _, ok := b.subs[s]; ok {
			delete(b.subs, s)
			close(s.ch)
		}
	}
}
