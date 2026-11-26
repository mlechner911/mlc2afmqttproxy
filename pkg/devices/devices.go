// Package devices beobachtet die Zigbee-Geräte hinter zigbee2mqtt: welche es gibt (aus
// <base>/bridge/devices), wie oft jedes sendet, wann zuletzt, mit welcher Batterie und
// Linkqualität und ob z2m es als erreichbar meldet (<base>/<name>/availability).
//
// Gezählt wird jede empfangene Nachricht eines Geräts — vor Filter und Deduplizierung, denn
// gefragt ist, wie oft das Gerät sendet, nicht was weitergeht. Die Zählerstände holt die
// Verlaufsstatistik regelmäßig ab (Drain) und verbucht sie je Stunde und Tag.
package devices

import (
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// DefaultBaseTopic ist das Basis-Topic von zigbee2mqtt (z2m: mqtt.base_topic).
const DefaultBaseTopic = "zigbee2mqtt"

// Device ist, was über ein Gerät bekannt ist.
type Device struct {
	Name        string `json:"name" example:"Wohnzimmer Temperatur"`
	IEEE        string `json:"ieee,omitempty" example:"0x00158d0000000001"`
	Model       string `json:"model,omitempty"`
	Vendor      string `json:"vendor,omitempty"`
	Description string `json:"description,omitempty"`
	// Type: Coordinator, Router oder EndDevice (z2m).
	Type        string `json:"type,omitempty"`
	PowerSource string `json:"power_source,omitempty"`
	// Availability: online/offline laut z2m; leer, wenn z2m keine Verfügbarkeit meldet.
	Availability string    `json:"availability,omitempty"`
	LastSeen     time.Time `json:"last_seen,omitzero"`
	Battery      *float64  `json:"battery,omitempty"`
	LinkQuality  *float64  `json:"linkquality,omitempty"`
	// Known: in bridge/devices aufgeführt (sonst nur aus einem Topic erschlossen).
	Known bool `json:"known"`
	// Writable: per <name>/set schreibbare Eigenschaften laut z2m-Definition (exposes).
	Writable []string `json:"writable,omitempty"`
}

// Tracker sammelt Gerätedaten aus den empfangenen Nachrichten. Sicher für parallele Aufrufe.
type Tracker struct {
	prefix string // "<base>/"

	mu      sync.Mutex
	devices map[string]*Device // Name → Gerät
	pending map[string]int64   // Name → Nachrichten seit dem letzten Drain
	dirty   bool
}

// New legt einen Tracker für das Basis-Topic base an (leer = DefaultBaseTopic).
func New(base string) *Tracker {
	if base == "" {
		base = DefaultBaseTopic
	}
	return &Tracker{prefix: strings.TrimSuffix(base, "/") + "/",
		devices: map[string]*Device{}, pending: map[string]int64{}}
}

// Observe verarbeitet eine empfangene Nachricht. Andere Topics als die von zigbee2mqtt werden
// übergangen.
func (t *Tracker) Observe(topic string, payload []byte, at time.Time) {
	rest, ok := strings.CutPrefix(topic, t.prefix)
	if !ok || rest == "" {
		return
	}
	if rest == "bridge/devices" {
		t.setBridgeDevices(payload)
		return
	}
	if strings.HasPrefix(rest, "bridge/") {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if name, ok := strings.CutSuffix(rest, "/availability"); ok && t.isDevice(name) {
		t.device(name).Availability = availability(payload)
		t.dirty = true
		return
	}
	if !t.isDevice(rest) {
		return // z. B. <name>/set oder <name>/get — Befehle an das Gerät, keine Meldung von ihm
	}
	d := t.device(rest)
	d.LastSeen = at
	readState(d, payload)
	t.pending[rest]++
	t.dirty = true
}

// isDevice: rest ist ein Gerätename — bekannt aus bridge/devices, oder (solange die Liste fehlt)
// ein Name ohne „/“.
func (t *Tracker) isDevice(rest string) bool {
	if _, ok := t.devices[rest]; ok {
		return true
	}
	return !t.anyKnown() && !strings.Contains(rest, "/")
}

func (t *Tracker) anyKnown() bool {
	for _, d := range t.devices {
		if d.Known {
			return true
		}
	}
	return false
}

func (t *Tracker) device(name string) *Device {
	d := t.devices[name]
	if d == nil {
		d = &Device{Name: name}
		t.devices[name] = d
	}
	return d
}

// ByIEEE sucht ein Gerät über seine IEEE-Adresse (Kopie); ok ist false, wenn es nicht in
// bridge/devices steht.
func (t *Tracker) ByIEEE(ieee string) (Device, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, d := range t.devices {
		if d.Known && strings.EqualFold(d.IEEE, ieee) {
			return *d, true
		}
	}
	return Device{}, false
}

// Drain liefert die Nachrichten je Gerät seit dem letzten Aufruf und setzt sie zurück.
func (t *Tracker) Drain() map[string]int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := t.pending
	t.pending = map[string]int64{}
	return out
}

// List liefert alle Geräte (Kopien), ohne den Koordinator.
func (t *Tracker) List() []Device {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Device, 0, len(t.devices))
	for _, d := range t.devices {
		if d.Type != "Coordinator" {
			out = append(out, *d)
		}
	}
	return out
}

// readState übernimmt Batterie und Linkqualität aus einer JSON-Meldung des Geräts.
func readState(d *Device, payload []byte) {
	var v struct {
		Battery     *float64 `json:"battery"`
		LinkQuality *float64 `json:"linkquality"`
	}
	if json.Unmarshal(payload, &v) != nil {
		return
	}
	if v.Battery != nil {
		d.Battery = v.Battery
	}
	if v.LinkQuality != nil {
		d.LinkQuality = v.LinkQuality
	}
}

// availability liest {"state":"online"} (z2m ab 1.x) oder den nackten Text „online“.
func availability(payload []byte) string {
	var v struct {
		State string `json:"state"`
	}
	if json.Unmarshal(payload, &v) == nil && v.State != "" {
		return v.State
	}
	return strings.TrimSpace(string(payload))
}
