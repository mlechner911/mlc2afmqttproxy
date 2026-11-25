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
