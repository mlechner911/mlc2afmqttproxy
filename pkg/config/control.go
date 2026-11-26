package config

import (
	"fmt"
	"strings"
)

// validateControl prüft die Freigaben des Rückwegs fail closed: Eine Freigabe, die nicht greifen
// kann oder mehr erlaubt als gedacht, verhindert den Start, statt still ignoriert zu werden.
func validateControl(cfg *Config) error {
	if cfg.Control != nil && len(cfg.Control.Devices) > 0 {
		if err := checkFilter("control.subscribe", cfg.Control.Subscribe); err != nil {
			return err
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
		if err := checkFilter("routes["+r.Name+"].control_subscribe", r.ControlSubscribe); err != nil {
			return err
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

// checkFilter prüft einen Topic-Filter (leer = Vorgabe cmd/zb/#): # nur als letztes Segment,
// Wildcards nur als ganzes Segment.
func checkFilter(where, f string) error {
	if f == "" {
		return nil
	}
	segs := strings.Split(f, "/")
	for i, s := range segs {
		switch {
		case s == "#" && i != len(segs)-1:
			return fmt.Errorf("%s %q: # nur am Ende", where, f)
		case s != "#" && s != "+" && strings.ContainsAny(s, "#+"):
			return fmt.Errorf("%s %q: Wildcards nur als ganzes Segment", where, f)
		case strings.ContainsAny(s, " "):
			return fmt.Errorf("%s %q: Leerzeichen im Topic", where, f)
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
