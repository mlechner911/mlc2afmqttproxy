package config

import (
	"strings"
	"testing"
)

func TestValidateControl(t *testing.T) {
	tests := []struct {
		name, yaml, wantErr string
	}{
		{"aus", "mode: mqtt\n", ""},
		{"ok", "monitor:\n  customer_number: \"4111\"\ncontrol:\n  devices:\n    \"0xa4c138eeaab187e0\": [state]\n", ""},
		{"nichts konfiguriert", "mode: mqtt\n", ""},
		{"allow writable", "control:\n  allow: writable\n", ""},
		{"allow kaputt", "control:\n  allow: all\n", "erlaubt sind"},
		{"Route allow kaputt", "routes:\n  - name: demo\n    control_allow: alles\n", "erlaubt sind"},
		{"ohne Kundennummer", "control:\n  devices:\n    \"0xa4c138eeaab187e0\": [state]\n", ""},
		{"eigenes Abo", "control:\n  subscribe: \"cmd/zb/4111/+/set\"\n  devices:\n    \"0x01\": [state]\n", ""},
		{"Abo # in der Mitte", "control:\n  subscribe: \"cmd/#/set\"\n  devices:\n    \"0x01\": [state]\n", "nur am Ende"},
		{"Name statt IEEE", "monitor:\n  customer_number: \"4111\"\ncontrol:\n  devices:\n    \"Flur Licht\": [state]\n", "IEEE"},
		{"ohne Felder", "monitor:\n  customer_number: \"4111\"\ncontrol:\n  devices:\n    \"0x01\": []\n", "ohne Felder"},
		{"Feld mit Wildcard", "monitor:\n  customer_number: \"4111\"\ncontrol:\n  devices:\n    \"0x01\": [\"#\"]\n", "ungültiges Feld"},
		{"Route ok", "routes:\n  - name: demo\n    customer_number: \"2641\"\n    devices: [\"0x01\", \"0x02\"]\n    control:\n      \"0x01\": [state]\n", ""},
		{"Route ohne Kundennummer", "routes:\n  - name: demo\n    control:\n      \"0x01\": [state]\n", ""},
		{"Route Abo kaputt", "routes:\n  - name: demo\n    control_subscribe: \"cmd/zb+/#\"\n    control:\n      \"0x01\": [state]\n", "ganzes Segment"},
		{"Route fremdes Gerät", "routes:\n  - name: demo\n    customer_number: \"2641\"\n    devices: [\"0x01\"]\n    control:\n      \"0x09\": [state]\n", "eigenen Geräte"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := load(t, tt.yaml)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("unerwarteter Fehler: %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("Fehler = %v, soll %q enthalten", err, tt.wantErr)
			}
		})
	}
}
