package svcmon

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

// fakeMsg ist eine eingehende Steuer-Nachricht.
type fakeMsg struct {
	payload  string
	retained bool
}

func (m fakeMsg) Duplicate() bool   { return false }
func (m fakeMsg) Qos() byte         { return 1 }
func (m fakeMsg) Retained() bool    { return m.retained }
func (m fakeMsg) Topic() string     { return "cmd/svc/edgeproxy" }
func (m fakeMsg) MessageID() uint16 { return 1 }
func (m fakeMsg) Payload() []byte   { return []byte(m.payload) }
func (m fakeMsg) Ack()              {}

// TestRetainedCommandIgnored (P3-03): Ein gespeicherter Befehl käme bei jedem Verbinden erneut —
// ein retained „stop" beendete den Proxy nach jedem Neustart. Er wird verworfen.
func TestRetainedCommandIgnored(t *testing.T) {
	stopped := make(chan struct{}, 1)
	r := New(Options{Device: "svc-edgeproxy", OnStop: func() { stopped <- struct{}{} }})
	r.onCommand(nil, fakeMsg{payload: `{"action":"stop"}`, retained: true})
	r.onCommand(nil, fakeMsg{payload: `{"action":"pause"}`, retained: true})
	select {
	case <-stopped:
		t.Fatal("retained stop ausgeführt")
	case <-time.After(700 * time.Millisecond):
	}
	if r.Paused() {
		t.Fatal("retained pause ausgeführt")
	}
	r.onCommand(nil, fakeMsg{payload: `{"action":"stop"}`})
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("frischer stop nicht ausgeführt")
	}
}

// TestPauseEndsAfterMaxPause (P3-03): Eine vergessene Pause füllte den Puffer bis zum Tail-Drop.
// Nach der Höchstdauer läuft der Versand von selbst wieder; resume hebt sie sofort auf.
func TestPauseEndsAfterMaxPause(t *testing.T) {
	for _, action := range []string{"pause", "drain"} {
		r := New(Options{Device: "svc-edgeproxy", MaxPause: 150 * time.Millisecond})
		r.onCommand(nil, fakeMsg{payload: `{"action":"` + action + `"}`})
		if !r.Paused() {
			t.Fatalf("%s: nicht pausiert", action)
		}
		time.Sleep(200 * time.Millisecond)
		if r.Paused() {
			t.Fatalf("%s: nach Höchstdauer noch pausiert", action)
		}
		if r.state.Load() != StateRunning {
			t.Fatalf("%s: Zustand %d, want läuft", action, r.state.Load())
		}
	}
	r := New(Options{Device: "svc-edgeproxy", MaxPause: time.Hour})
	r.onCommand(nil, fakeMsg{payload: `{"action":"pause"}`})
	r.onCommand(nil, fakeMsg{payload: `{"action":"resume"}`})
	if r.Paused() {
		t.Fatal("resume hebt die Pause nicht auf")
	}
}

// TestCmdTopicPerCustomer (T-SENS-20261107-02.1): Mit Kundennummer hört die Box auf den Zweig
// ihres Mandanten — der Monitor lässt eine Kunden-Verbindung seit 12.11.2026 nichts anderes
// mehr lesen; ohne Nummer (Eigenbetrieb) auf das Plattform-Topic wie bisher.
func TestCmdTopicPerCustomer(t *testing.T) {
	if got := New(Options{Device: "svc-edgeproxy", Customer: "4111"}).cmdTopic(); got != "cmd/svc/4111/edgeproxy" {
		t.Errorf("mit Kundennummer: %s", got)
	}
	if got := New(Options{Device: "svc-edgeproxy"}).cmdTopic(); got != "cmd/svc/edgeproxy" {
		t.Errorf("Eigenbetrieb: %s", got)
	}
}
