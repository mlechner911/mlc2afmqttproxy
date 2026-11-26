// Package control ist der Rückweg vom MLC Sensor Monitor zu zigbee2mqtt (Plan P-AFMQ-20261126-01).
//
// Der Proxy schaltet nie selbst: keine Regeln, keine Grenzwerte auf der Box. Er nimmt Befehle
// auf …/<ieee>/set an (Abo je Upstream, Vorgabe cmd/zb/# — welche Befehle ankommen, entscheidet der
// Upstream), prüft sie gegen die Freigabe in der config.yaml und gegen
// das, was das Gerät laut zigbee2mqtt (bridge/devices → exposes) beschreiben lässt, reicht sie an
// <base>/<gerät>/set weiter und quittiert auf demselben Topic mit /ack statt /set — „ok“ erst, wenn das Gerät den neuen Zustand
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
	// AckTopic: das Befehls-Topic mit /ack statt /set — leer, wenn das Topic kein Gerät nennt.
	AckTopic string
}

// IsCommand: Das Topic ist ein Befehl (…/<ieee>/set). Alles andere unter dem Abo — etwa die
// eigenen Quittungen — wird still übergangen.
func IsCommand(topic string) bool { return strings.HasSuffix(topic, "/set") }

// ParseRequest liest die IEEE-Adresse aus dem Topic (das Segment vor /set) und Felder samt _id
// aus dem Payload. _id wird entfernt — es geht nicht an zigbee2mqtt.
func ParseRequest(topic string, payload []byte) (Request, string) {
	req := Request{}
	if rest, ok := strings.CutSuffix(topic, "/set"); ok {
		if i := strings.LastIndex(rest, "/"); i >= 0 && i < len(rest)-1 {
			req.IEEE = rest[i+1:]
			req.AckTopic = rest + "/ack"
		}
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

// DefaultFields sind die Bedienfelder, die ohne Konfiguration schaltbar sind: an/aus, Helligkeit,
// Farbe, Rollladenposition — dazu state_<kanal> mehrkanaliger Schalter (state_l1, …).
// Einstellungen am Gerät (child_lock, power_outage_memory, Kalibrierung, Offsets …) gehören nicht
// dazu und brauchen eine ausdrückliche Freigabe (control.devices bzw. routes[].control).
var DefaultFields = []string{"state", "brightness", "color_temp", "color", "position"}

// IsDefaultField: f ist ohne Konfiguration schaltbar.
func IsDefaultField(f string) bool {
	return contains(DefaultFields, f) || (strings.HasPrefix(f, "state_") && len(f) > len("state_"))
}

// Allow-Modi (control.allow bzw. routes[].control_allow).
const (
	// AllowOperate (Vorgabe): Bedienfelder an jedem Gerät, dazu die Zusatzfelder aus der Liste.
	AllowOperate = "operate"
	// AllowWritable: jedes Feld, das das Gerät selbst als setzbar meldet (auch Einstellungen,
	// Kalibrierung) — für vorinstallierte Stationen, an denen der Monitor auch kalibriert.
	AllowWritable = "writable"
	// AllowList: nur Geräte und Felder aus der Liste (der strenge Weg, z. B. neben Home Assistant).
	AllowList = "list"
)

// Policy entscheidet, ob ein Befehl weitergereicht wird. Was ein Mandant überhaupt schalten soll,
// entscheidet der Monitor (nur der Server schreibt cmd/zb, nur in den Zweig des Mandanten); die
// Policy begrenzt es auf Bedienfelder, auf Geräte der Route und auf das, was das Gerät kann.
type Policy struct {
	mode      string
	extra     map[string]map[string]bool // ieee (klein) → Felder aus der Liste
	only      map[string]bool            // nil = alle Geräte; sonst nur diese (Route)
	lookup    func(ieee string) (devices.Device, bool)
	perMinute int

	mu   sync.Mutex
	sent map[string][]time.Time // ieee → Zeitpunkte der letzten Minute
}

// NewPolicy: mode = AllowOperate (leer) | AllowWritable | AllowList; extra = Felder je Gerät aus
// der Liste (IEEE → Felder); only = die Geräte, die der Mandant schalten darf (leer = alle, die
// zigbee2mqtt kennt).
func NewPolicy(mode string, extra map[string][]string, only []string, lookup func(string) (devices.Device, bool), perMinute int) *Policy {
	if perMinute <= 0 {
		perMinute = 10
	}
	if mode == "" {
		mode = AllowOperate
	}
	p := &Policy{mode: mode, extra: map[string]map[string]bool{}, lookup: lookup, perMinute: perMinute, sent: map[string][]time.Time{}}
	for ieee, fields := range extra {
		set := map[string]bool{}
		for _, f := range fields {
			set[f] = true
		}
		p.extra[strings.ToLower(ieee)] = set
	}
	if len(only) > 0 {
		p.only = map[string]bool{}
		for _, ieee := range only {
			p.only[strings.ToLower(ieee)] = true
		}
	}
	return p
}

// Mode liefert den Allow-Modus.
func (p *Policy) Mode() string { return p.mode }

// Scope beschreibt die Freigabe für Anzeige: Felder je Gerät aus der Liste (sortiert) und die
// Geräteliste (nil = alle).
func (p *Policy) Scope() (extra map[string][]string, only []string) {
	extra = map[string][]string{}
	for ieee, set := range p.extra {
		for f := range set {
			extra[ieee] = append(extra[ieee], f)
		}
		sort.Strings(extra[ieee])
	}
	for ieee := range p.only {
		only = append(only, ieee)
	}
	sort.Strings(only)
	return extra, only
}

// Check prüft Gerät, Felder, Schreibbarkeit, Erreichbarkeit und das Rate-Limit. Bei Erfolg
// liefert es das Gerät (für den friendly_name im z2m-Topic) und zählt den Befehl.
func (p *Policy) Check(req Request, now time.Time) (devices.Device, string) {
	ieee := strings.ToLower(req.IEEE)
	if p.only != nil && !p.only[ieee] {
		return devices.Device{}, ReasonNotAllowed
	}
	if _, listed := p.extra[ieee]; p.mode == AllowList && !listed {
		return devices.Device{}, ReasonNotAllowed
	}
	for _, f := range sortedKeys(req.Fields) {
		if !p.fieldAllowed(ieee, f) {
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
	if !p.take(ieee, now) {
		return devices.Device{}, ReasonRateLimit
	}
	return dev, ""
}

// fieldAllowed: Liste immer; Bedienfelder außer im Listen-Modus; im Modus writable alles (ob das
// Gerät das Feld wirklich setzen kann, prüft Check danach für jeden Modus).
func (p *Policy) fieldAllowed(ieee, f string) bool {
	switch {
	case p.extra[ieee][f]:
		return true
	case p.mode == AllowWritable:
		return true
	case p.mode == AllowOperate:
		return IsDefaultField(f)
	}
	return false
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
