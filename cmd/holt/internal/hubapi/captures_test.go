package hubapi_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openotters/holt/cmd/holt/internal/capture"
	"github.com/openotters/holt/cmd/holt/internal/hubapi"
)

// managerStub answers from canned state, keeping these tests about the
// HTTP contract only.
type managerStub struct {
	bins      []capture.Bin
	createErr error

	createdName string
	createdTTL  time.Duration
	stopped     string
}

func (m *managerStub) Create(name string, ttl time.Duration) (capture.Bin, error) {
	m.createdName, m.createdTTL = name, ttl

	if m.createErr != nil {
		return capture.Bin{}, m.createErr
	}

	return capture.Bin{Peer: "capture-abc123", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (m *managerStub) List() []capture.Bin { return m.bins }

func (m *managerStub) Stop(name string) bool {
	m.stopped = name

	return name != "missing"
}

func capturesMux(stub *managerStub) *http.ServeMux {
	mux := http.NewServeMux()
	hubapi.Captures{Manager: stub}.Mount(mux)

	return mux
}

// An empty body is the console's one-click case: generated name,
// default TTL.
func TestCreateCaptureWithEmptyBody(t *testing.T) {
	t.Parallel()

	stub := &managerStub{}

	rec := post(t, capturesMux(stub), "/api/captures", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}

	if stub.createdName != "" || stub.createdTTL != 0 {
		t.Fatalf("Create called with (%q, %v), want defaults", stub.createdName, stub.createdTTL)
	}

	var bin capture.Bin
	if err := json.Unmarshal(rec.Body.Bytes(), &bin); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}

	if bin.Peer == "" {
		t.Fatal("response names no peer")
	}
}

func TestCreateCapturePassesBodyAndErrors(t *testing.T) {
	t.Parallel()

	stub := &managerStub{}

	rec := post(t, capturesMux(stub), "/api/captures", `{"peer":"mybin","ttlSeconds":120}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}

	if stub.createdName != "mybin" || stub.createdTTL != 2*time.Minute {
		t.Fatalf("Create called with (%q, %v), want (mybin, 2m)", stub.createdName, stub.createdTTL)
	}

	stub.createErr = errors.New("a peer named \"mybin\" is attached")

	rec = post(t, capturesMux(stub), "/api/captures", `{"peer":"mybin"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", rec.Code)
	}
}

func TestListAndDeleteCaptures(t *testing.T) {
	t.Parallel()

	stub := &managerStub{bins: []capture.Bin{{Peer: "capture-abc123"}}}
	mux := capturesMux(stub)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/captures", nil))

	var got struct {
		Captures []capture.Bin `json:"captures"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}

	if len(got.Captures) != 1 || got.Captures[0].Peer != "capture-abc123" {
		t.Fatalf("list = %+v, want the one live endpoint", got.Captures)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/captures/capture-abc123", nil))

	if rec.Code != http.StatusNoContent || stub.stopped != "capture-abc123" {
		t.Fatalf("delete: status %d, stopped %q; want 204 for capture-abc123", rec.Code, stub.stopped)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/captures/missing", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing: status %d, want 404", rec.Code)
	}
}
