package svcmon

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestName prüft die Ableitung des cmd/svc-Namens aus dem Gerätenamen.
func TestName(t *testing.T) {
	if r := New(Options{Device: "svc-edgeproxy"}); r.name != "edgeproxy" {
		t.Fatalf("name = %q; want edgeproxy", r.name)
	}
	if r := New(Options{Device: "edgeproxy"}); r.name != "edgeproxy" {
		t.Fatalf("name = %q; want edgeproxy (ohne Präfix unverändert)", r.name)
	}
}

// TestPaused prüft, dass pause/drain den Versand ruhen lassen, running nicht.
func TestPaused(t *testing.T) {
	r := New(Options{Device: "svc-edgeproxy"})
	if r.Paused() {
		t.Fatal("frisch erzeugt darf nicht pausiert sein")
	}
	r.state.Store(StatePaused)
	if !r.Paused() {
		t.Fatal("StatePaused → Paused() muss true sein")
	}
	r.state.Store(StateDraining)
	if !r.Paused() {
		t.Fatal("StateDraining → Paused() muss true sein")
	}
	r.state.Store(StateRunning)
	if r.Paused() {
		t.Fatal("StateRunning → Paused() muss false sein")
	}
}

// TestHeartbeat prüft Ziel, Header und Payload des Heartbeats inkl. service_state.
func TestHeartbeat(t *testing.T) {
	var gotToken string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotToken = req.Header.Get("X-Ingest-Token")
		b, _ := io.ReadAll(req.Body)
		_ = json.Unmarshal(b, &body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	r := New(Options{Device: "svc-edgeproxy", IngestURL: srv.URL, Token: "secret-token"})
	r.state.Store(StatePaused) // muss als service_state=2 erscheinen
	r.sendHeartbeat()

	if gotToken != "secret-token" {
		t.Fatalf("X-Ingest-Token = %q; want secret-token", gotToken)
	}
	if body["device"] != "svc-edgeproxy" {
		t.Fatalf("device = %v; want svc-edgeproxy", body["device"])
	}
	readings, ok := body["readings"].([]any)
	if !ok || len(readings) != 3 {
		t.Fatalf("readings = %v; want 3 Einträge", body["readings"])
	}
	vals := map[string]float64{}
	for _, ri := range readings {
		m := ri.(map[string]any)
		vals[m["key"].(string)] = m["value"].(float64)
	}
	if vals["service_up"] != 1 {
		t.Fatalf("service_up = %v; want 1", vals["service_up"])
	}
	if vals["service_state"] != 2 {
		t.Fatalf("service_state = %v; want 2 (paused)", vals["service_state"])
	}
	if _, ok := vals["svc_uptime_s"]; !ok {
		t.Fatal("svc_uptime_s fehlt")
	}
}
