package forwarder

import "testing"

// TestClientIDPerRoute: Ohne Einstellung bleibt die bisherige ID (Haupt-Upstream unverändert);
// eine Route setzt eine eigene. Zwei Verbindungen mit derselben ID werfen sich am Broker
// gegenseitig hinaus und können dem falschen Mandanten zugeordnet werden (12.10.2026).
func TestClientIDPerRoute(t *testing.T) {
	f5 := NewMQTT5Forwarder("tcp://127.0.0.1:1", "", "", false, nil)
	if got := f5.clientIDOr("mlc2af-proxy-forwarder-v5"); got != "mlc2af-proxy-forwarder-v5" {
		t.Errorf("Standard-ID: %q", got)
	}
	f5.SetClientID("mlc2af-proxy-forwarder-route-demo")
	if got := f5.clientIDOr("mlc2af-proxy-forwarder-v5"); got != "mlc2af-proxy-forwarder-route-demo" {
		t.Errorf("Routen-ID: %q", got)
	}
	f3 := NewMQTTForwarder("tcp://127.0.0.1:1", "", "", "none", "_ts", nil)
	f3.SetClientID("x")
	if f3.clientIDOr("d") != "x" {
		t.Error("v3: eigene ID nicht übernommen")
	}
}
