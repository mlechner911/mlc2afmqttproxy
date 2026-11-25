package devices

import (
	"path/filepath"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

const bridgeList = `[
 {"ieee_address":"0x01","friendly_name":"Coordinator","type":"Coordinator"},
 {"ieee_address":"0x02","friendly_name":"Bad/Fenster","type":"EndDevice","power_source":"Battery",
  "definition":{"model":"MCCGQ11LM","vendor":"Aqara","description":"Door sensor"}},
 {"ieee_address":"0x03","friendly_name":"Flur","type":"Router","power_source":"Mains (single phase)"}
]`

func TestObserve(t *testing.T) {
	tr := New("")
	tr.Observe("zigbee2mqtt/bridge/devices", []byte(bridgeList), now)
	tr.Observe("zigbee2mqtt/Bad/Fenster", []byte(`{"contact":true,"battery":87,"linkquality":120}`), now)
	tr.Observe("zigbee2mqtt/Bad/Fenster", []byte(`{"contact":false}`), now.Add(time.Minute))
	tr.Observe("zigbee2mqtt/Bad/Fenster/availability", []byte(`{"state":"offline"}`), now)
	tr.Observe("zigbee2mqtt/Flur/set", []byte(`{"state":"ON"}`), now) // Befehl, keine Meldung
	tr.Observe("zigbee2mqtt/Unbekannt", []byte(`{}`), now)            // nicht in der Liste
	tr.Observe("zigbee2mqtt/bridge/state", []byte(`{"state":"online"}`), now)
	tr.Observe("anderes/topic", []byte(`1`), now)

	counts := tr.Drain()
	if len(counts) != 1 || counts["Bad/Fenster"] != 2 {
		t.Fatalf("Zähler = %v; nur Bad/Fenster mit 2", counts)
	}
	if again := tr.Drain(); len(again) != 0 {
		t.Fatalf("Drain setzt nicht zurück: %v", again)
	}
	byName := map[string]Device{}
	for _, d := range tr.List() {
		byName[d.Name] = d
	}
	if _, ok := byName["Coordinator"]; ok || len(byName) != 2 {
		t.Fatalf("Liste = %v; zwei Geräte ohne Koordinator", byName)
	}
	d := byName["Bad/Fenster"]
	if d.Model != "MCCGQ11LM" || d.Availability != "offline" || *d.Battery != 87 || *d.LinkQuality != 120 ||
		!d.LastSeen.Equal(now.Add(time.Minute)) || !d.Known {
		t.Fatalf("Gerät = %+v", d)
	}
}

func TestWithoutBridgeList(t *testing.T) {
	tr := New("z2m")
	tr.Observe("z2m/sensor1", []byte(`{"temperature":21}`), now)
	tr.Observe("z2m/sensor1/availability", []byte(`online`), now)
	tr.Observe("z2m/sensor1/set", []byte(`{}`), now)
	if c := tr.Drain(); c["sensor1"] != 1 || len(c) != 1 {
		t.Fatalf("ohne bridge/devices: %v", c)
	}
	if l := tr.List(); len(l) != 1 || l[0].Availability != "online" || l[0].Known {
		t.Fatalf("Liste = %+v", l)
	}
}

func TestSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	tr := New("")
	tr.Observe("zigbee2mqtt/bridge/devices", []byte(bridgeList), now)
	tr.Observe("zigbee2mqtt/Flur", []byte(`{"linkquality":200}`), now)
	if err := tr.Save(path); err != nil {
		t.Fatal(err)
	}
	r := New("")
	if err := r.Load(path); err != nil {
		t.Fatal(err)
	}
	if err := New("").Load(filepath.Join(t.TempDir(), "fehlt.json")); err != nil {
		t.Fatalf("fehlende Datei ist kein Fehler: %v", err)
	}
	for _, d := range r.List() {
		if d.Name == "Flur" && (!d.LastSeen.Equal(now) || *d.LinkQuality != 200) {
			t.Fatalf("nach Laden: %+v", d)
		}
	}
	// nach Laden mit Liste: Unterverzeichnis-Namen weiter richtig erkannt
	r.Observe("zigbee2mqtt/Bad/Fenster", []byte(`{}`), now)
	if r.Drain()["Bad/Fenster"] != 1 {
		t.Fatal("Bad/Fenster nach Laden nicht erkannt")
	}
}
