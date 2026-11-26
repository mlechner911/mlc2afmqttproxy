package control

import (
	"strings"
	"testing"
	"time"

	"mlc2afmqttproxy/pkg/devices"
)

var plug = devices.Device{Name: "Steckdose Flur", IEEE: "0xa4c138eeaab187e0", Known: true,
	Writable: []string{"child_lock", "countdown", "power_outage_memory", "state"}}

func lookup(ieee string) (devices.Device, bool) {
	switch strings.ToLower(ieee) {
	case plug.IEEE:
		return plug, true
	case "0xoffline":
		return devices.Device{Name: "aus", IEEE: "0xoffline", Known: true, Availability: "offline", Writable: []string{"state"}}, true
	}
	return devices.Device{}, false
}

func TestParseRequest(t *testing.T) {
	tests := []struct {
		name, payload, reason, id string
		fields                    int
	}{
		{"ok", `{"state":"OFF","_id":"u1"}`, "", "u1", 1},
		{"ohne _id", `{"state":"OFF"}`, ReasonMissingID, "", 1},
		{"kein Objekt", `"OFF"`, ReasonInvalid, "", 0},
		{"kaputt", `{`, ReasonInvalid, "", 0},
		{"nur _id", `{"_id":"u1"}`, ReasonNoFields, "u1", 0},
	}
	for _, tt := range tests {
		req, reason := ParseRequest("cmd/zb/4111/0xa4c138eeaab187e0/set", []byte(tt.payload))
		if reason != tt.reason || req.ID != tt.id || len(req.Fields) != tt.fields {
			t.Errorf("%s: reason=%q id=%q fields=%v", tt.name, reason, req.ID, req.Fields)
		}
		if _, has := req.Fields["_id"]; has {
			t.Errorf("%s: _id darf nicht an zigbee2mqtt gehen", tt.name)
		}
		if tt.reason == "" && req.IEEE != "0xa4c138eeaab187e0" {
			t.Errorf("%s: IEEE = %q", tt.name, req.IEEE)
		}
	}
	for topic, want := range map[string][2]string{
		"cmd/zb/4111/0x01/set":   {"0x01", "cmd/zb/4111/0x01/ack"},
		"cmd/zb/0x01/set":        {"0x01", "cmd/zb/0x01/ack"},
		"ha/a/b/c/0x02/set":      {"0x02", "ha/a/b/c/0x02/ack"},
		"cmd/zb/4111/0x01/state": {"", ""},
		"set":                    {"", ""},
	} {
		req, _ := ParseRequest(topic, []byte(`{"state":"ON","_id":"x"}`))
		if req.IEEE != want[0] || req.AckTopic != want[1] {
			t.Errorf("%s: IEEE=%q Ack=%q, erwartet %v", topic, req.IEEE, req.AckTopic, want)
		}
	}
}

func TestPolicyCheck(t *testing.T) {
	// Ohne Konfiguration: Bedienfelder an allen Geräten; child_lock nur mit Zusatzfreigabe.
	p := NewPolicy("", nil, nil, lookup, 2)
	now := time.Now()
	req := func(ieee string, f map[string]any) Request { return Request{IEEE: ieee, ID: "x", Fields: f} }
	tests := []struct {
		name   string
		r      Request
		reason string
	}{
		{"ok", req(plug.IEEE, map[string]any{"state": "ON"}), ""},
		{"Einstellung ohne Freigabe", req(plug.IEEE, map[string]any{"child_lock": "LOCK"}), "field not allowed: child_lock"},
		{"Bedienfeld, Gerät kann es nicht", req(plug.IEEE, map[string]any{"brightness": 100}), "field not writable on this device: brightness"},
		{"z2m kennt es nicht", req("0xunknown", map[string]any{"state": "ON"}), ReasonUnknown},
		{"offline", req("0xoffline", map[string]any{"state": "ON"}), ReasonOffline},
		{"zweiter ok", req(plug.IEEE, map[string]any{"state": "OFF"}), ""},
		{"Rate-Limit (2/min)", req(plug.IEEE, map[string]any{"state": "ON"}), ReasonRateLimit},
	}
	for _, tt := range tests {
		dev, reason := p.Check(tt.r, now)
		if reason != tt.reason {
			t.Errorf("%s: reason=%q, soll %q", tt.name, reason, tt.reason)
		}
		if reason == "" && dev.Name != plug.Name {
			t.Errorf("%s: Gerät %q", tt.name, dev.Name)
		}
	}
	if _, reason := p.Check(req(plug.IEEE, map[string]any{"state": "ON"}), now.Add(61*time.Second)); reason != "" {
		t.Errorf("nach einer Minute wieder frei: %q", reason)
	}
}

// TestPolicyScope: Route nur mit ihren Geräten; Zusatzfreigabe für eine Einstellung.
func TestPolicyScope(t *testing.T) {
	p := NewPolicy(AllowOperate, map[string][]string{"0xA4C138EEAAB187E0": {"child_lock"}}, []string{"0xA4C138EEAAB187E0"}, lookup, 10)
	now := time.Now()
	for _, tt := range []struct {
		ieee   string
		f      map[string]any
		reason string
	}{
		{plug.IEEE, map[string]any{"child_lock": "LOCK"}, ""},
		{plug.IEEE, map[string]any{"power_outage_memory": "on"}, "field not allowed: power_outage_memory"},
		{"0xoffline", map[string]any{"state": "ON"}, ReasonNotAllowed}, // nicht in der Route
	} {
		if _, r := p.Check(Request{IEEE: tt.ieee, ID: "x", Fields: tt.f}, now); r != tt.reason {
			t.Errorf("%s %v: %q, soll %q", tt.ieee, tt.f, r, tt.reason)
		}
	}
	for f, ok := range map[string]bool{"state": true, "state_l2": true, "state_": false, "position": true, "child_lock": false, "local_temperature_calibration": false} {
		if IsDefaultField(f) != ok {
			t.Errorf("IsDefaultField(%q) = %v", f, !ok)
		}
	}
}

// TestPolicyModes: writable = alles Setzbare; list = nur die Liste, sonst nichts.
func TestPolicyModes(t *testing.T) {
	now := time.Now()
	w := NewPolicy(AllowWritable, nil, nil, lookup, 10)
	if _, r := w.Check(Request{IEEE: plug.IEEE, ID: "x", Fields: map[string]any{"power_outage_memory": "on"}}, now); r != "" {
		t.Errorf("writable: %q", r)
	}
	if _, r := w.Check(Request{IEEE: plug.IEEE, ID: "x", Fields: map[string]any{"brightness": 10}}, now); r != "field not writable on this device: brightness" {
		t.Errorf("writable, Gerät kann es nicht: %q", r)
	}
	l := NewPolicy(AllowList, map[string][]string{"0xoffline": {"state"}}, nil, lookup, 10)
	if _, r := l.Check(Request{IEEE: plug.IEEE, ID: "x", Fields: map[string]any{"state": "ON"}}, now); r != ReasonNotAllowed {
		t.Errorf("list, nicht gelistet: %q", r)
	}
	if _, r := l.Check(Request{IEEE: "0xoffline", ID: "x", Fields: map[string]any{"brightness": 1}}, now); r != "field not allowed: brightness" {
		t.Errorf("list, Feld nicht gelistet: %q", r)
	}
}

func TestMatches(t *testing.T) {
	tests := []struct {
		want, got map[string]any
		ok        bool
	}{
		{map[string]any{"state": "ON"}, map[string]any{"state": "ON", "power": 10.0}, true},
		{map[string]any{"state": "on"}, map[string]any{"state": "ON"}, true},
		{map[string]any{"state": "ON"}, map[string]any{"state": "OFF"}, false},
		{map[string]any{"state": "TOGGLE"}, map[string]any{"state": "OFF"}, true},
		{map[string]any{"brightness": 128.0}, map[string]any{"brightness": 127.0}, true},
		{map[string]any{"brightness": 200.0}, map[string]any{"brightness": 150.0}, false},
		{map[string]any{"state": "ON"}, map[string]any{"power": 1.0}, false},
	}
	for i, tt := range tests {
		if Matches(tt.want, tt.got) != tt.ok {
			t.Errorf("Fall %d: %v gegen %v", i, tt.want, tt.got)
		}
	}
}

func TestWaiters(t *testing.T) {
	w := NewWaiters("zigbee2mqtt")
	done, cancel := w.Add("Steckdose Flur", map[string]any{"state": "OFF"})
	w.Observe("zigbee2mqtt/Steckdose Flur", []byte(`{"state":"ON","power":10}`)) // Echo des alten Zustands
	select {
	case <-done:
		t.Fatal("ON darf OFF nicht bestätigen")
	default:
	}
	w.Observe("zigbee2mqtt/anderes", []byte(`{"state":"OFF"}`))
	w.Observe("zigbee2mqtt/Steckdose Flur", []byte(`{"state":"OFF","power":0}`))
	select {
	case st := <-done:
		if st["power"] != 0.0 {
			t.Fatalf("Zustand %v", st)
		}
	case <-time.After(time.Second):
		t.Fatal("keine Bestätigung")
	}
	cancel()
	_, cancel2 := w.Add("x", map[string]any{"state": "ON"})
	w.Observe("zigbee2mqtt/x", []byte(`{"state":"OFF"}`))
	if last := cancel2(); last["state"] != "OFF" {
		t.Fatalf("Timeout soll den letzten Zustand liefern: %v", last)
	}
}
