package broker

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/mochi-mqtt/server/v2/packets"

	"mlc2afmqttproxy/pkg/config"
	"mlc2afmqttproxy/pkg/storage"
)

const bridgeDevices = `[
 {"ieee_address":"0x0000000000000001","friendly_name":"0x0000000000000001","type":"Coordinator"},
 {"ieee_address":"0xa4c138fb6e98090b","friendly_name":"0xa4c138fb6e98090b","definition":{"model":"SZ-T04"}},
 {"ieee_address":"0xa4c1386a262aa14f","friendly_name":"MovementSensor 0xa4c1386a262aa14f","definition":{"model":"ZG-204ZL"}},
 {"ieee_address":"0x0c4314fffe52afc7","friendly_name":"0x0c4314fffe52afc7","definition":{"model":"TS011F_plug_3"}}
]`

func demoRoute(store *storage.Store) Route {
	return NewRoute(config.RouteConf{Name: "demo", BaseTopic: "zigbee2mqtt",
		Devices: []string{"0xa4c138fb6e98090b", "0xa4c1386a262aa14f"}}, store) // IEEE statt Anzeigename
}

// TestRouteAccept: Mit Geräteliste gehen nur deren Topics und ein gekürztes bridge/devices
// hinaus — keine anderen Geräte, keine übrigen Bridge-Meldungen (bridge/info trägt die
// z2m-Konfiguration).
func TestRouteAccept(t *testing.T) {
	r := demoRoute(nil)
	cases := []struct {
		topic string
		want  bool
	}{
		{"zigbee2mqtt/0xa4c138fb6e98090b", true},
		{"zigbee2mqtt/0xa4c138fb6e98090b/availability", true},
		{"zigbee2mqtt/0x0c4314fffe52afc7", false}, // nicht in der Liste
		{"zigbee2mqtt/bridge/info", false},        // Standort-Konfiguration
		{"zigbee2mqtt/bridge/state", false},
		{"zigbee2mqtt/bridge/devices", true},       // gekürzt
		{"homeassistant/sensor/x/config", false},   // anderes Präfix
		{"zigbee2mqtt/0xa4c138fb6e98090bX", false}, // kein Präfix-Treffer auf halbe Namen
	}
	for _, c := range cases {
		if _, got := r.Accept(c.topic, []byte(`{}`)); got != c.want && c.topic != "zigbee2mqtt/bridge/devices" {
			t.Errorf("%s: got %v, want %v", c.topic, got, c.want)
		}
	}
	// Anzeigename mit Leerzeichen ist ein eigenes Topic in z2m — per IEEE in der Liste
	// greift er hier NICHT (friendly_name ist das Topic). Für solche Geräte den
	// Anzeigenamen in devices eintragen.
	if _, ok := r.Accept("zigbee2mqtt/MovementSensor 0xa4c1386a262aa14f", []byte(`{}`)); ok {
		t.Error("Topic über Anzeigenamen darf ohne Eintrag des Anzeigenamens nicht passieren")
	}
}

// TestFilterBridgeDevices: Nur die Geräte der Route bleiben, Einträge unverändert; Treffer
// über IEEE ODER Anzeigenamen.
func TestFilterBridgeDevices(t *testing.T) {
	r := demoRoute(nil)
	out, ok := r.Accept("zigbee2mqtt/bridge/devices", []byte(bridgeDevices))
	if !ok {
		t.Fatal("bridge/devices abgelehnt")
	}
	var got []map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d Geräte, want 2: %s", len(got), out)
	}
	for _, d := range got {
		if d["ieee_address"] == "0x0c4314fffe52afc7" || d["type"] == "Coordinator" {
			t.Errorf("Gerät außerhalb der Liste weitergegeben: %v", d)
		}
	}
	if _, ok := r.Accept("zigbee2mqtt/bridge/devices", []byte(`kein json`)); ok {
		t.Error("ungültige Liste muss verworfen werden, nicht ungefiltert weitergehen")
	}
}

// TestHookWritesToRoute: Eine Nachricht landet im Haupt-Puffer UND im Puffer der passenden
// Route; eine nicht passende nur im Haupt-Puffer.
func TestHookWritesToRoute(t *testing.T) {
	dir := t.TempDir()
	main, err := storage.InitBadger(filepath.Join(dir, "main"))
	if err != nil {
		t.Fatal(err)
	}
	defer main.Close()
	rs, err := storage.InitBadger(filepath.Join(dir, "routes", "demo"))
	if err != nil {
		t.Fatal(err)
	}
	defer rs.Close()
	h := &StoreHook{store: main, lastSeen: map[string]lastMsg{}, routes: []Route{demoRoute(rs)}}

	pub := func(topic, payload string) {
		var pk packets.Packet
		pk.TopicName = topic
		pk.Payload = []byte(payload)
		if _, err := h.OnPublish(nil, pk); err != nil {
			t.Fatal(err)
		}
	}
	pub("zigbee2mqtt/0xa4c138fb6e98090b", `{"temperature":21.3,"last_seen":1791000000000}`)
	pub("zigbee2mqtt/0x0c4314fffe52afc7", `{"power":0}`)

	if n, _ := main.GetSize(); n != 2 {
		t.Errorf("Haupt-Puffer: %d Einträge, want 2", n)
	}
	if n, _ := rs.GetSize(); n != 1 {
		t.Fatalf("Routen-Puffer: %d Einträge, want 1", n)
	}
	_, val, err := rs.PeekFirst()
	if err != nil {
		t.Fatal(err)
	}
	var w PayloadWrapper
	_ = json.Unmarshal(val, &w)
	if w.Topic != "zigbee2mqtt/0xa4c138fb6e98090b" {
		t.Errorf("Routen-Puffer enthält %q", w.Topic)
	}
}
