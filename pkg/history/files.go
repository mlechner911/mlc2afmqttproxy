package history

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Ablage unter dir: days/<Monat>.json, slots/<Tag>.json, spans/<Monat>.json.
const (
	daysDir  = "days"
	slotsDir = "slots"
	spansDir = "spans"
)

// readFiles liest jede JSON-Datei in sub nach into. Eine kaputte Datei wird in .broken
// umbenannt und übersprungen — ein Lesefehler soll den Proxy nicht aufhalten.
func readFiles[T any](dir, sub string, into func([]T)) error {
	paths, err := filepath.Glob(filepath.Join(dir, sub, "*.json"))
	if err != nil {
		return fmt.Errorf("history %s: %w", sub, err)
	}
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("history lesen %s: %w", p, err)
		}
		var items []T
		if err := json.Unmarshal(raw, &items); err != nil {
			log.Printf("[history] %s ist kaputt (%v) — als .broken beiseitegelegt", p, err)
			_ = os.Rename(p, p+".broken")
			continue
		}
		into(items)
	}
	return nil
}

// writeFile schreibt v atomar (Temp-Datei + Rename) nach dir/sub/name.json; ein leeres v
// löscht die Datei.
func writeFile(dir, sub, name string, v any, empty bool) error {
	path := filepath.Join(dir, sub, name+".json")
	if empty {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("history löschen %s: %w", path, err)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("history verzeichnis: %w", err)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("history serialisieren: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o640); err != nil {
		return fmt.Errorf("history schreiben %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("history umbenennen %s: %w", path, err)
	}
	return nil
}

func (s *Store) loadDays() error {
	return readFiles(s.dir, daysDir, func(items []Day) {
		for i := range items {
			d := items[i]
			s.days[d.Date] = &d
		}
	})
}

func (s *Store) loadSlots() error {
	return readFiles(s.dir, slotsDir, func(items []Slot) {
		for i := range items {
			sl := items[i]
			s.slots[sl.Start.Unix()] = &sl
		}
	})
}

func (s *Store) loadSpans() error {
	err := readFiles(s.dir, spansDir, func(items []Span) { s.spans = append(s.spans, items...) })
	sort.Slice(s.spans, func(i, j int) bool { return s.spans[i].Start.Before(s.spans[j].Start) })
	s.closeOpenSpans()
	return err
}

// Flush räumt Abgelaufenes weg und schreibt, was sich geändert hat. now bestimmt die Grenzen
// der Aufbewahrung.
func (s *Store) Flush(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune(now)
	var errs []error
	for month := range s.dirtyDays {
		days := s.daysWithPrefix(month)
		errs = append(errs, writeFile(s.dir, daysDir, month, days, len(days) == 0))
	}
	for date := range s.dirtySlots {
		slots := s.slotsOn(date, now.Location())
		errs = append(errs, writeFile(s.dir, slotsDir, date, slots, len(slots) == 0))
	}
	for month := range s.dirtySpans {
		spans := s.spansStartingIn(month)
		errs = append(errs, writeFile(s.dir, spansDir, month, spans, len(spans) == 0))
	}
	clear(s.dirtyDays)
	clear(s.dirtySlots)
	clear(s.dirtySpans)
	errs = append(errs, s.flushDevices(now))
	return errors.Join(errs...)
}

// prune entfernt Tageswerte und Intervalle älter als DefaultRetentionDays und Abschnitte älter
// als DefaultSlotDays; betroffene Dateien werden als geändert markiert (und dann gelöscht).
func (s *Store) prune(now time.Time) {
	dayLimit := now.AddDate(0, 0, -DefaultRetentionDays).Format(dateLayout)
	for date := range s.days {
		if date < dayLimit {
			delete(s.days, date)
			s.dirtyDays[date[:len(monthLayout)]] = true
		}
	}
	slotLimit := now.AddDate(0, 0, -DefaultSlotDays)
	for k, sl := range s.slots {
		if sl.Start.Before(slotLimit) {
			delete(s.slots, k)
			s.dirtySlots[sl.Start.In(now.Location()).Format(dateLayout)] = true
		}
	}
	spanLimit := now.AddDate(0, 0, -DefaultRetentionDays)
	kept := s.spans[:0]
	for _, sp := range s.spans {
		if sp.End.Before(spanLimit) {
			s.markSpan(sp)
			continue
		}
		kept = append(kept, sp)
	}
	s.spans = kept
}

func (s *Store) daysWithPrefix(month string) []Day {
	var out []Day
	for date, d := range s.days {
		if strings.HasPrefix(date, month) {
			out = append(out, *d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}

func (s *Store) slotsOn(date string, loc *time.Location) []Slot {
	var out []Slot
	for _, sl := range s.slots {
		if sl.Start.In(loc).Format(dateLayout) == date {
			out = append(out, *sl)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out
}

func (s *Store) spansStartingIn(month string) []Span {
	var out []Span
	for _, sp := range s.spans {
		if sp.Start.Format(monthLayout) == month {
			out = append(out, sp)
		}
	}
	return out
}
