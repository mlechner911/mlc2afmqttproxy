package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func load(t *testing.T, yaml string) (*Config, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(p, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return LoadConfig(p)
}

// TestLocalBrokerBindsLoopbackByDefault (P3-01): Ohne Angabe lauscht der lokale Broker nur auf
// 127.0.0.1. Bis 07.11.2026 auf allen Schnittstellen und ohne Anmeldung.
func TestLocalBrokerBindsLoopbackByDefault(t *testing.T) {
	cfg, err := load(t, "mode: mqtt\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MQTT.Bind != "127.0.0.1" {
		t.Errorf("mqtt.bind = %q, want 127.0.0.1", cfg.MQTT.Bind)
	}
}

// TestOpenBindNeedsLocalUsers: Von außen erreichbar ohne Konto → Start verweigert (fail closed).
func TestOpenBindNeedsLocalUsers(t *testing.T) {
	for _, bind := range []string{"0.0.0.0", "::", "192.168.1.20"} {
		_, err := load(t, "mqtt:\n  bind: \""+bind+"\"\n")
		if err == nil || !strings.Contains(err.Error(), "local_users") {
			t.Errorf("bind %s ohne Konto: err = %v, want Ablehnung", bind, err)
		}
		// Ein Konto ohne Passwort zählt nicht.
		if _, err := load(t, "mqtt:\n  bind: \""+bind+"\"\n  local_users:\n    - username: z2m\n"); err == nil {
			t.Errorf("bind %s mit Konto ohne Passwort angenommen", bind)
		}
		cfg, err := load(t, "mqtt:\n  bind: \""+bind+"\"\n  local_users:\n    - username: z2m\n      password: geheim-im-test\n")
		if err != nil || cfg.MQTT.Bind != bind {
			t.Errorf("bind %s mit Konto: err = %v", bind, err)
		}
	}
}

// TestDashboardWithoutPasswordStaysLocal (P3-10): Ohne Passwort nur 127.0.0.1, mit Passwort die
// eingestellte Adresse (Fernzugriff zum Testen).
func TestDashboardWithoutPasswordStaysLocal(t *testing.T) {
	cfg, err := load(t, "server:\n  port: 8097\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("ohne Passwort: host = %q, want 127.0.0.1", cfg.Server.Host)
	}
	cfg, err = load(t, "server:\n  host: \"0.0.0.0\"\n  username: mlc\n  password: geheim-im-test\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("mit Passwort: host = %q, want 0.0.0.0", cfg.Server.Host)
	}
}

func TestIsLoopbackHost(t *testing.T) {
	for h, want := range map[string]bool{
		"127.0.0.1": true, "127.0.0.2": true, "::1": true, "[::1]": true, "localhost": true,
		"": false, "0.0.0.0": false, "::": false, "192.168.1.20": false, "box.local": false,
	} {
		if got := IsLoopbackHost(h); got != want {
			t.Errorf("IsLoopbackHost(%q) = %v, want %v", h, got, want)
		}
	}
}
