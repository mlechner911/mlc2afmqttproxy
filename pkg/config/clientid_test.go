package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestUpstreamClientIDPerBox (P3-04): Zwei Boxen bekommen verschiedene Client-IDs (aus
// /etc/machine-id); client_id überschreibt; ohne machine-id gilt der Hostname.
func TestUpstreamClientIDPerBox(t *testing.T) {
	alt := machineIDPath
	t.Cleanup(func() { machineIDPath = alt })
	dir := t.TempDir()
	id := func(machineID string) string {
		p := filepath.Join(dir, machineID)
		if err := os.WriteFile(p, []byte(machineID+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		machineIDPath = p
		return (&Config{}).UpstreamClientID()
	}
	a, b := id("4f1c2a9e0b7d4c3e8a1f2b3c4d5e6f70"), id("9a8b7c6d5e4f30211234567890abcdef")
	if a == b {
		t.Fatalf("zwei Boxen mit derselben Client-ID %q", a)
	}
	if a != "mlc2af-proxy-4f1c2a9e0b7d" {
		t.Errorf("Client-ID %q", a)
	}
	c := &Config{}
	c.MQTT.ClientID = "kunde-lager-1"
	if got := c.UpstreamClientID(); got != "kunde-lager-1" {
		t.Errorf("client_id überschreibt nicht: %q", got)
	}
	machineIDPath = filepath.Join(dir, "gibt-es-nicht")
	h, _ := os.Hostname()
	if got := (&Config{}).UpstreamClientID(); h != "" && got != "mlc2af-proxy-"+h {
		t.Errorf("ohne machine-id: %q, want Hostname", got)
	}
}
