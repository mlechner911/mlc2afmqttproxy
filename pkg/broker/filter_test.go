package broker

import (
	"path/filepath"
	"testing"

	"github.com/mochi-mqtt/server/v2/packets"

	"mlc2afmqttproxy/pkg/config"
	"mlc2afmqttproxy/pkg/metrics"
	"mlc2afmqttproxy/pkg/storage"
)

// TestFilterHitsAttributed: Ein Topic, das die Ignorierliste ausdrücklich nennt, zählt für diese
// Regel — nicht pauschal für die Positivliste (bis 25.11.2026 lief die Positivliste zuerst).
func TestFilterHitsAttributed(t *testing.T) {
	st, err := storage.InitBadger(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	h := &StoreHook{store: st, lastSeen: map[string]lastMsg{}, filter: &config.FilterConf{
		AllowedPrefixes: []string{"zigbee2mqtt/"},
		IgnoredPrefixes: []string{"homeassistant/", "zigbee2mqtt/bridge/logging"},
	}}
	before := metrics.FilterHits()
	for _, topic := range []string{
		"homeassistant/sensor/0x01/temperature/config", // ausdrücklich ignoriert
		"zigbee2mqtt/bridge/logging",                   // ausdrücklich ignoriert
		"anderes/topic",                                // nicht erlaubt
		"zigbee2mqtt/0x01",                             // geht durch
	} {
		var pk packets.Packet
		pk.TopicName, pk.Payload = topic, []byte(`{}`)
		if _, err := h.OnPublish(nil, pk); err != nil {
			t.Fatal(err)
		}
	}
	after := metrics.FilterHits()
	for rule, want := range map[string]uint64{"ignore:homeassistant/": 1, "ignore:zigbee2mqtt/bridge/logging": 1, "allow": 1} {
		if got := after[rule] - before[rule]; got != want {
			t.Errorf("Treffer %s = %d, soll %d", rule, got, want)
		}
	}
	if n, _ := st.GetSize(); n != 1 {
		t.Errorf("gepuffert: %d, soll 1 (nur zigbee2mqtt/0x01)", n)
	}
}

// TestCommandsNotBuffered: Befehle an zigbee2mqtt sind keine Messwerte und gehen nicht nach oben.
func TestCommandsNotBuffered(t *testing.T) {
	SetCommandBase("zigbee2mqtt")
	for topic, want := range map[string]bool{
		"zigbee2mqtt/0x01/set":                    true,
		"zigbee2mqtt/0x01/set/led_indicator":      true,
		"zigbee2mqtt/0x01/get":                    true,
		"zigbee2mqtt/Flur/Licht/set":              true,
		"zigbee2mqtt/bridge/request/permit_join":  true,
		"zigbee2mqtt/0x01":                        false,
		"zigbee2mqtt/0x01/availability":           false,
		"zigbee2mqtt/bridge/devices":              false,
		"zigbee2mqtt/bridge/response/permit_join": false,
		"zigbee2mqtt/set":                         false, // Gerät heißt „set“
		"anderes/0x01/set":                        false,
	} {
		if got := isCommand(nil, topic); got != want {
			t.Errorf("%s: %v, soll %v", topic, got, want)
		}
	}
	st, err := storage.InitBadger(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	h := &StoreHook{store: st, lastSeen: map[string]lastMsg{}}
	var pk packets.Packet
	pk.TopicName, pk.Payload = "zigbee2mqtt/0x01/set", []byte(`{"state":"ON"}`)
	if _, err := h.OnPublish(nil, pk); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.GetSize(); n != 0 {
		t.Fatalf("Befehl gepuffert: %d", n)
	}
}
