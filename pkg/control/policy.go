// Package control ist der Rückweg vom MLC Sensor Monitor zu zigbee2mqtt (Plan P-AFMQ-20261126-01).
//
// Der Proxy schaltet nie selbst: keine Regeln, keine Grenzwerte auf der Box. Er nimmt Befehle
// auf cmd/zb/<kundennummer>/<ieee>/set an, prüft sie gegen die Freigabe in der config.yaml und gegen
// das, was das Gerät laut zigbee2mqtt (bridge/devices → exposes) beschreiben lässt, reicht sie an
// <base>/<gerät>/set weiter und quittiert auf …/ack — „ok“ erst, wenn das Gerät den neuen Zustand
// meldet. Befehle werden nie gepuffert: Was nicht sofort geht, wird abgelehnt.
package control

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"mlc2afmqttproxy/pkg/devices"
)

// Gründe für eine Ablehnung (Feld reason der Quittung).
const (
	ReasonInvalid     = "invalid payload: JSON object expected"
	ReasonMissingID   = "missing _id"
	ReasonNotAllowed  = "device not allowed"
	ReasonUnknown     = "device unknown to zigbee2mqtt"
	ReasonOffline     = "device offline"
	ReasonRateLimit   = "rate limit"
	ReasonNoZigbee    = "zigbee2mqtt not reachable"
	ReasonNoFields    = "no fields to set"
	reasonFieldPrefix = "field not allowed: "
	reasonWritePrefix = "field not writable on this device: "
)

// Request ist ein angenommener, noch ungeprüfter Befehl.
type Request struct {
	IEEE   string
	ID     string
	Fields map[string]any
}

// ParseRequest liest IEEE aus dem Topic (cmd/zb/<kunde>/<ieee>/set) und Felder samt _id aus dem
// Payload. _id wird entfernt — es geht nicht an zigbee2mqtt.
func ParseRequest(topic string, payload []byte) (Request, string) {
	parts := strings.Split(topic, "/")
	req := Request{}
	if len(parts) == 5 {
		req.IEEE = parts[3]
	}
	var fields map[string]any
	if json.Unmarshal(payload, &fields) != nil || fields == nil {
		return req, ReasonInvalid
	}
	if id, ok := fields["_id"].(string); ok {
		req.ID = id
	}
	delete(fields, "_id")
	req.Fields = fields
	if req.ID == "" {
		return req, ReasonMissingID
	}
	if len(fields) == 0 {
		return req, ReasonNoFields
	}
	return req, ""
}

// Policy entscheidet, ob ein Befehl weitergereicht wird.
type Policy struct {
	allow     map[string]map[string]bool // ieee (klein) → erlaubte Felder
	lookup    func(ieee string) (devices.Device, bool)
	perMinute int

	mu   sync.Mutex
	sent map[string][]time.Time // ieee → Zeitpunkte der letzten Minute
}

// NewPolicy baut die Freigabe aus der Konfiguration (IEEE → Felder).
func NewPolicy(allow map[string][]string, lookup func(string) (devices.Device, bool), perMinute int) *Policy {
	if perMinute <= 0 {
		perMinute = 10
	}
	p := &Policy{allow: map[string]map[string]bool{}, lookup: lookup, perMinute: perMinute, sent: map[string][]time.Time{}}
	for ieee, fields := range allow {
		set := map[string]bool{}
		for _, f := range fields {
			set[f] = true
		}
		p.allow[strings.ToLower(ieee)] = set
	}
	return p
}

// Allowed liefert die Freigabe (IEEE → Felder, sortiert) für Anzeige und Heartbeat.
func (p *Policy) Allowed() map[string][]string {
	out := map[string][]string{}
	for ieee, set := range p.allow {
		for f := range set {
			out[ieee] = append(out[ieee], f)
		}
		sort.Strings(out[ieee])
	}
	return out
}

// Check prüft Freigabe, Schreibbarkeit, Erreichbarkeit des Geräts und das Rate-Limit. Bei Erfolg
// liefert es das Gerät (für den friendly_name im z2m-Topic) und zählt den Befehl.
func (p *Policy) Check(req Request, now time.Time) (devices.Device, string) {
	allowed, ok := p.allow[strings.ToLower(req.IEEE)]
	if !ok {
		return devices.Device{}, ReasonNotAllowed
	}
	for _, f := range sortedKeys(req.Fields) {
		if !allowed[f] {
			return devices.Device{}, reasonFieldPrefix + f
		}
	}
	dev, ok := p.lookup(req.IEEE)
	if !ok {
		return devices.Device{}, ReasonUnknown
	}
	for _, f := range sortedKeys(req.Fields) {
		if !contains(dev.Writable, f) {
			return devices.Device{}, reasonWritePrefix + f
		}
	}
	if dev.Availability == "offline" {
		return devices.Device{}, ReasonOffline
	}
	if !p.take(strings.ToLower(req.IEEE), now) {
		return devices.Device{}, ReasonRateLimit
	}
	return dev, ""
}

// take zählt einen Befehl, wenn das Gerät in der letzten Minute weniger als perMinute hatte.
func (p *Policy) take(ieee string, now time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	recent := p.sent[ieee][:0]
	for _, t := range p.sent[ieee] {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	if len(recent) >= p.perMinute {
		p.sent[ieee] = recent
		return false
	}
	p.sent[ieee] = append(recent, now)
	return true
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Matches meldet, ob ein gemeldeter Zustand die gewünschten Felder trägt. Texte ohne Groß/klein
// („on“ = „ON“), Zahlen mit Toleranz (Geräte runden Helligkeit u. ä.), TOGGLE passt auf jeden Wert.
func Matches(want, got map[string]any) bool {
	for k, w := range want {
		g, ok := got[k]
		if !ok {
			return false
		}
		if !valueMatches(w, g) {
			return false
		}
	}
	return true
}

func valueMatches(w, g any) bool {
	switch wv := w.(type) {
	case string:
		gs, ok := g.(string)
		return ok && (strings.EqualFold(wv, "TOGGLE") || strings.EqualFold(wv, gs))
	case float64:
		gf, ok := g.(float64)
		if !ok {
			return false
		}
		tol := 1.0
		if t := 0.02 * abs(wv); t > tol {
			tol = t
		}
		return abs(wv-gf) <= tol
	default:
		return fmt.Sprint(w) == fmt.Sprint(g)
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
