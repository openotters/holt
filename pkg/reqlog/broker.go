package reqlog

import (
	"context"
	"sync"
)

// Broker fans one Hook out to many watchers. Nothing is persisted: the
// last Recent events are kept in memory so a watcher arriving
// mid-traffic is not blank, and a watcher that falls behind loses
// events rather than slowing the request that produced them.
//
//	broker := reqlog.NewBroker(0)
//	proxy := revproxy.New(reg, revproxy.WithRequestHook(broker.Hook()))
//
//	for ev := range broker.Watch(ctx) { ... }
type Broker struct {
	mu     sync.Mutex
	subs   map[int]chan Event
	nextID int

	// recent is a ring of the last events, oldest first once it wraps.
	recent []Event
	at     int
	full   bool
}

// DefaultRecent is how many events a new watcher is replayed when the
// broker is built with 0. Enough to fill a console panel, small enough
// to forget.
const DefaultRecent = 100

// watchChanSize buffers each watcher. Sends never block: a watcher
// that cannot keep up drops events instead of holding the request.
const watchChanSize = 64

// NewBroker returns a broker replaying the last recent events to each
// new watcher. Pass 0 for DefaultRecent, or a negative number for no
// replay at all (a watcher then sees only what arrives after it).
func NewBroker(recent int) *Broker {
	switch {
	case recent == 0:
		recent = DefaultRecent
	case recent < 0:
		recent = 0
	}

	return &Broker{subs: map[int]chan Event{}, recent: make([]Event, recent)}
}

// Hook returns the Hook that feeds the broker, for
// revproxy.WithRequestHook or dial.Options.RequestHook.
func (b *Broker) Hook() Hook { return b.Publish }

// Publish records an event and hands it to every watcher.
func (b *Broker) Publish(ev Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.remember(ev)

	for _, ch := range b.subs {
		select {
		case ch <- ev:
		default: // the request never waits for a lagging watcher
		}
	}
}

// remember stores ev in the ring. Callers hold b.mu.
func (b *Broker) remember(ev Event) {
	if len(b.recent) == 0 {
		return
	}

	b.recent[b.at] = ev
	b.at = (b.at + 1) % len(b.recent)

	if b.at == 0 {
		b.full = true
	}
}

// Watch streams events until ctx ends, starting with the recent ones
// the broker still holds (oldest first). The channel is closed when
// ctx does.
func (b *Broker) Watch(ctx context.Context) <-chan Event {
	ch := make(chan Event, watchChanSize)

	b.mu.Lock()
	id := b.nextID
	b.nextID++
	b.subs[id] = ch

	replay := b.recentLocked()
	b.mu.Unlock()

	// The replay goes out on the same channel, before anything live,
	// so a watcher reads one ordered stream. It runs in the caller's
	// goroutine only as far as the buffer allows; the rest waits for
	// the reader, which is why the live sends above never block.
	go func() {
		for _, ev := range replay {
			select {
			case ch <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		<-ctx.Done()

		b.mu.Lock()
		defer b.mu.Unlock()

		delete(b.subs, id)
		close(ch)
	}()

	return ch
}

// recentLocked copies the ring out in order, oldest first. Callers
// hold b.mu.
func (b *Broker) recentLocked() []Event {
	if len(b.recent) == 0 {
		return nil
	}

	if !b.full {
		return append([]Event(nil), b.recent[:b.at]...)
	}

	out := make([]Event, 0, len(b.recent))
	out = append(out, b.recent[b.at:]...)

	return append(out, b.recent[:b.at]...)
}
