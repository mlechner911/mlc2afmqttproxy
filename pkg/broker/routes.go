package broker

import (
	"encoding/json"
	"strings"
	"sync"

	"mlc2afmqttproxy/pkg/config"
	"mlc2afmqttproxy/pkg/storage"
)

// Route ist ein zusätzlicher Upstream mit eigenem Puffer (siehe config.RouteConf). Der
// StoreHook schreibt eine Nachricht, die der globale Filter und die Entprellung passiert hat,
// zusätzlich in den Puffer jeder Route, deren Regeln sie erfüllt.
type Route struct {
	Name    string
	Store   *storage.Store
	Filter  *config.FilterConf
	Base    string
	devices map[string]bool // friendly_name oder IEEE; leer = alle
	// names: aktuelle friendly_names der Geräte der Route, aus dem jüngsten bridge/devices.
	// Die Topics tragen den friendly_name; ein Eintrag per IEEE ließe ein umbenanntes Gerät
	// sonst nicht mehr durch (B-20261012-03). Zeiger, weil Route als Wert kopiert wird.
	names *liveNames
}

type liveNames struct {
	mu  sync.RWMutex
	set map[string]bool
}

func (n *liveNames) has(name string) bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.set[name]
}

func (n *liveNames) replace(set map[string]bool) {
	n.mu.Lock()
	n.set = set
	n.mu.Unlock()
}

// NewRoute baut eine Route aus ihrer Konfiguration und ihrem Puffer.
func NewRoute(rc config.RouteConf, store *storage.Store) Route {
	r := Route{Name: rc.Name, Store: store, Filter: rc.Filter, Base: rc.BaseTopic}
	if r.Base == "" {
		r.Base = "zigbee2mqtt"
	}
	if len(rc.Devices) > 0 {
		r.names = &liveNames{}
		r.devices = make(map[string]bool, len(rc.Devices))
		for _, d := range rc.Devices {
			r.devices[d] = true
		}
	}
	return r
}

// Accept entscheidet, ob topic/payload in diese Route gehört, und liefert die zu
// speichernde Nutzlast (bei bridge/devices auf die Geräte der Route gekürzt).
func (r Route) Accept(topic string, payload []byte) ([]byte, bool) {
	if !prefixAllowed(r.Filter, topic) {
		return nil, false
	}
	if r.devices == nil {
		return payload, true
	}
	rest, ok := strings.CutPrefix(topic, r.Base+"/")
	if !ok {
		return nil, false
	}
	if rest == "bridge/devices" {
		out, names, err := filterBridgeDevices(payload, r.devices)
		if err != nil {
			return nil, false // lieber nichts als die ganze Liste
		}
		r.names.replace(names)
		return out, true
	}
	if strings.HasPrefix(rest, "bridge/") {
		// Andere Bridge-Meldungen (info, state, logging, …) gehören zum Standort, nicht zu
		// den Geräten dieser Route — bridge/info trägt z. B. die z2m-Konfiguration.
		return nil, false
	}
	dev := rest
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		dev = rest[:i] // <gerät>/availability, <gerät>/set …
	}
	// Bis zur ersten Geräteliste gilt nur der Eintrag selbst.
	return payload, r.devices[dev] || r.names.has(dev)
}

// prefixAllowed wendet allowed/ignored_prefixes an (nil = alles erlaubt).
func prefixAllowed(f *config.FilterConf, topic string) bool {
	if f == nil {
		return true
	}
	if len(f.AllowedPrefixes) > 0 {
		ok := false
		for _, p := range f.AllowedPrefixes {
			if strings.HasPrefix(topic, p) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	for _, p := range f.IgnoredPrefixes {
		if strings.HasPrefix(topic, p) {
			return false
		}
	}
	return true
}

// filterBridgeDevices kürzt die z2m-Geräteliste auf die Geräte der Route. Einträge bleiben
// unverändert (als rohes JSON), nur die Auswahl ändert sich. Dazu die friendly_names der
// behaltenen Geräte — unter denen kommen ihre Werte.
func filterBridgeDevices(payload []byte, devices map[string]bool) ([]byte, map[string]bool, error) {
	var all []json.RawMessage
	if err := json.Unmarshal(payload, &all); err != nil {
		return nil, nil, err
	}
	names := make(map[string]bool, len(devices))
	keep := make([]json.RawMessage, 0, len(devices))
	for _, raw := range all {
		var d struct {
			IEEE     string `json:"ieee_address"`
			Friendly string `json:"friendly_name"`
		}
		if json.Unmarshal(raw, &d) != nil {
			continue
		}
		if devices[d.Friendly] || devices[d.IEEE] {
			keep = append(keep, raw)
			names[d.Friendly] = true
		}
	}
	out, err := json.Marshal(keep)
	return out, names, err
}
