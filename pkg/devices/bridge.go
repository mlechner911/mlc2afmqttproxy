package devices

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// bridgeDevice ist ein Eintrag aus <base>/bridge/devices (nur die gebrauchten Felder).
type bridgeDevice struct {
	IEEE         string `json:"ieee_address"`
	FriendlyName string `json:"friendly_name"`
	Type         string `json:"type"`
	PowerSource  string `json:"power_source"`
	Definition   *struct {
		Model       string   `json:"model"`
		Vendor      string   `json:"vendor"`
		Description string   `json:"description"`
		Exposes     []expose `json:"exposes"`
	} `json:"definition"`
}

// expose ist ein Merkmal aus der z2m-Gerätedefinition; zusammengesetzte (light, switch …) tragen
// ihre Teile in Features.
type expose struct {
	Property string   `json:"property"`
	Access   int      `json:"access"`
	Features []expose `json:"features"`
}

// accessSet ist das Bit „per /set schreibbar“ in exposes.access (z2m: 1 state, 2 set, 4 get).
const accessSet = 2

// writable sammelt die per /set schreibbaren Eigenschaften, sortiert und ohne Doppelte.
func writable(ex []expose) []string {
	seen := map[string]bool{}
	var walk func([]expose)
	walk = func(list []expose) {
		for _, e := range list {
			walk(e.Features)
			if e.Property != "" && e.Access&accessSet != 0 {
				seen[e.Property] = true
			}
		}
	}
	walk(ex)
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// setBridgeDevices übernimmt die Geräteliste von zigbee2mqtt. Geräte, die nicht mehr darin
// stehen (entfernt oder umbenannt), fallen weg — ihr Verlauf bleibt in der Statistik.
func (t *Tracker) setBridgeDevices(payload []byte) {
	var list []bridgeDevice
	if json.Unmarshal(payload, &list) != nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	next := make(map[string]*Device, len(list))
	for _, b := range list {
		if b.FriendlyName == "" {
			continue
		}
		d := t.devices[b.FriendlyName]
		if d == nil {
			d = &Device{Name: b.FriendlyName}
		}
		d.IEEE, d.Type, d.PowerSource, d.Known = b.IEEE, b.Type, b.PowerSource, true
		if b.Definition != nil {
			d.Model, d.Vendor, d.Description = b.Definition.Model, b.Definition.Vendor, b.Definition.Description
			d.Writable = writable(b.Definition.Exposes)
		}
		next[b.FriendlyName] = d
	}
	t.devices = next
	t.dirty = true
}

// Load liest den gespeicherten Stand (Geräteliste, zuletzt gesehen …) aus path. Eine fehlende
// Datei ist kein Fehler: Nach dem ersten Start gibt es noch keine.
func (t *Tracker) Load(path string) error {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("geräte lesen: %w", err)
	}
	var list []Device
	if err := json.Unmarshal(raw, &list); err != nil {
		_ = os.Rename(path, path+".broken")
		return fmt.Errorf("geräte %s kaputt, beiseitegelegt: %w", path, err)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := range list {
		d := list[i]
		t.devices[d.Name] = &d
	}
	return nil
}

// Save schreibt den Stand atomar nach path, wenn sich seit dem letzten Speichern etwas geändert
// hat — damit „zuletzt gesehen“ und die Geräteliste einen Neustart überdauern (zigbee2mqtt
// schickt bridge/devices nur beim eigenen Start oder bei Änderungen).
func (t *Tracker) Save(path string) error {
	t.mu.Lock()
	if !t.dirty {
		t.mu.Unlock()
		return nil
	}
	list := make([]Device, 0, len(t.devices))
	for _, d := range t.devices {
		list = append(list, *d)
	}
	t.dirty = false
	t.mu.Unlock()
	if err := writeAtomic(path, list); err != nil {
		t.mu.Lock()
		t.dirty = true // beim nächsten Mal erneut versuchen
		t.mu.Unlock()
		return err
	}
	return nil
}

func writeAtomic(path string, list []Device) error {
	raw, err := json.Marshal(list)
	if err != nil {
		return fmt.Errorf("geräte serialisieren: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("geräte verzeichnis: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o640); err != nil {
		return fmt.Errorf("geräte schreiben: %w", err)
	}
	return os.Rename(tmp, path)
}
