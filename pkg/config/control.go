package config

import (
	"fmt"
	"strings"
)

// validateControl prüft die Freigaben des Rückwegs fail closed: Eine Freigabe, die nicht greifen
// kann oder mehr erlaubt als gedacht, verhindert den Start, statt still ignoriert zu werden.
func validateControl(cfg *Config) error {
	if cfg.Control != nil && len(cfg.Control.Devices) > 0 {
		if cfg.Monitor.CustomerNumber == "" {
			return fmt.Errorf("control.devices gesetzt, aber monitor.customer_number fehlt — ohne Kundennummer kein Befehlszweig cmd/zb/<kundennummer>/…")
		}
		if err := checkFields("control.devices", cfg.Control.Devices); err != nil {
			return err
		}
	}
	for _, r := range cfg.Routes {
		if len(r.Control) == 0 {
			continue
		}
		where := "routes[" + r.Name + "].control"
		if r.CustomerNumber == "" {
			return fmt.Errorf("%s gesetzt, aber customer_number der Route fehlt", where)
		}
		if err := checkFields(where, r.Control); err != nil {
			return err
		}
		if len(r.Devices) == 0 {
			continue
		}
		for ieee := range r.Control {
			if !containsFold(r.Devices, ieee) {
				return fmt.Errorf("%s: %s steht nicht in routes[%s].devices — die Route darf nur ihre eigenen Geräte schalten", where, ieee, r.Name)
			}
		}
	}
	return nil
}

func checkFields(where string, devices map[string][]string) error {
	for ieee, fields := range devices {
		if !strings.HasPrefix(strings.ToLower(ieee), "0x") {
			return fmt.Errorf("%s: %q ist keine IEEE-Adresse (0x…) — Befehle adressieren Geräte über die IEEE-Adresse", where, ieee)
		}
		if len(fields) == 0 {
			return fmt.Errorf("%s: %s ohne Felder — mindestens eines nennen, z. B. [state]", where, ieee)
		}
		for _, f := range fields {
			if f == "" || strings.ContainsAny(f, "/#+ ") {
				return fmt.Errorf("%s: %s: ungültiges Feld %q", where, ieee, f)
			}
		}
	}
	return nil
}

func containsFold(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}
