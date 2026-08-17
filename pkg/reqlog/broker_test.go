package reqlog_test

import (
	"testing"
	"time"

	"github.com/openotters/holt/pkg/reqlog"
)

// Every watcher gets every event, and a watcher that arrives late gets
// the window the broker still holds before the live ones.
func TestBrokerFansOutAndReplays(t *testing.T) {
	t.Parallel()

	broker := reqlog.NewBroker(0)

	early := broker.Watch(t.Context())
	broker.Publish(reqlog.Event{Path: "/first"})

	if ev := next(t, early); ev.Path != "/first" {
		t.Fatalf("early watcher got %q, want /first", ev.Path)
	}

	// A watcher that shows up after the fact still sees it.
	late := broker.Watch(t.Context())
	if ev := next(t, late); ev.Path != "/first" {
		t.Fatalf("late watcher replayed %q, want /first", ev.Path)
	}

	broker.Publish(reqlog.Event{Path: "/second"})

	for name, ch := range map[string]<-chan reqlog.Event{"early": early, "late": late} {
		if ev := next(t, ch); ev.Path != "/second" {
			t.Errorf("%s watcher got %q, want /second", name, ev.Path)
		}
	}
}

// The window is bounded: an old event falls out rather than the broker
// growing into a log nobody asked for.
func TestBrokerWindowIsBounded(t *testing.T) {
	t.Parallel()

	broker := reqlog.NewBroker(2)
	for _, path := range []string{"/a", "/b", "/c"} {
		broker.Publish(reqlog.Event{Path: path})
	}

	watcher := broker.Watch(t.Context())

	if ev := next(t, watcher); ev.Path != "/b" {
		t.Fatalf("replay started at %q, want /b (a fell out)", ev.Path)
	}

	if ev := next(t, watcher); ev.Path != "/c" {
		t.Fatalf("replay continued with %q, want /c", ev.Path)
	}
}

// With replay off a watcher sees only what happens next, which is the
// strictest reading of "live only".
func TestBrokerWithoutReplay(t *testing.T) {
	t.Parallel()

	broker := reqlog.NewBroker(-1)
	broker.Publish(reqlog.Event{Path: "/before"})

	watcher := broker.Watch(t.Context())
	broker.Publish(reqlog.Event{Path: "/after"})

	if ev := next(t, watcher); ev.Path != "/after" {
		t.Fatalf("got %q, want /after: nothing should be replayed", ev.Path)
	}
}

// Publishing with nobody watching is fine, and the hook is the same
// path as Publish (it is what the proxy is handed).
func TestBrokerHookWithoutWatchers(t *testing.T) {
	t.Parallel()

	broker := reqlog.NewBroker(0)
	broker.Hook()(reqlog.Event{Path: "/nobody-home"})

	if ev := next(t, broker.Watch(t.Context())); ev.Path != "/nobody-home" {
		t.Fatalf("hook did not record: got %q", ev.Path)
	}
}

// next takes the next event, failing the test if none arrives.
func next(t *testing.T, ch <-chan reqlog.Event) reqlog.Event {
	t.Helper()

	select {
	case ev := <-ch:
		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for an event")

		return reqlog.Event{}
	}
}
