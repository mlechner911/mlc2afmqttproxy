package history

import (
	"sort"
	"time"
)

// Period ist die Summe der letzten Days Tage (einschließlich heute).
type Period struct {
	Days int    `json:"days" example:"7"`
	From string `json:"from" example:"2026-11-19"`
	Counters
	BufferMax int64 `json:"buffer_max"`
	// Seconds je Zustand (State*) im Zeitraum; Zeit ohne Messung fehlt.
	Seconds map[string]int64 `json:"seconds"`
}

// PeriodLengths sind die Zeiträume der Übersicht: Tag, Woche, Monat, Jahr.
var PeriodLengths = []int{1, 7, 30, 365}

// Days liefert die Tageswerte der letzten n Tage bis now, älteste zuerst; Tage ohne Messung
// fehlen.
func (s *Store) Days(n int, now time.Time) []Day {
	s.mu.Lock()
	defer s.mu.Unlock()
	from := now.AddDate(0, 0, -(n - 1)).Format(dateLayout)
	var out []Day
	for date, d := range s.days {
		if date >= from {
			out = append(out, *d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}

// Summary liefert die Summen für jeden Zeitraum in PeriodLengths.
func (s *Store) Summary(now time.Time) []Period {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Period, 0, len(PeriodLengths))
	for _, n := range PeriodLengths {
		fromDay := now.AddDate(0, 0, -(n - 1))
		p := Period{Days: n, From: fromDay.Format(dateLayout)}
		for date, d := range s.days {
			if date >= p.From {
				p.add(d.Counters)
				p.BufferMax = max(p.BufferMax, d.BufferMax)
			}
		}
		y, m, dd := fromDay.Date()
		p.Seconds = secondsByState(s.spans, time.Date(y, m, dd, 0, 0, 0, 0, now.Location()), now)
		out = append(out, p)
	}
	return out
}

// Slots liefert die Abschnitte in [from, to), älteste zuerst.
func (s *Store) Slots(from, to time.Time) []Slot {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Slot
	for _, sl := range s.slots {
		if !sl.Start.Before(from) && sl.Start.Before(to) {
			out = append(out, *sl)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out
}

// Spans liefert die Zustandsintervalle, die [from, to) berühren, auf das Fenster gekürzt.
func (s *Store) Spans(from, to time.Time) []Span {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Span
	for _, sp := range s.spans {
		if sp.End.Before(from) || !sp.Start.Before(to) {
			continue
		}
		if sp.Start.Before(from) {
			sp.Start = from
		}
		if sp.End.After(to) {
			sp.End = to
		}
		out = append(out, sp)
	}
	return out
}

// Interruptions liefert die Zeiträume ohne Weiterleitung (alle Zustände außer ok) seit from,
// neueste zuerst, höchstens limit.
func (s *Store) Interruptions(from time.Time, limit int) []Span {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Span
	for i := len(s.spans) - 1; i >= 0 && len(out) < limit; i-- {
		sp := s.spans[i]
		if sp.End.Before(from) {
			break
		}
		if sp.State != StateOK {
			out = append(out, sp)
		}
	}
	return out
}

func secondsByState(spans []Span, from, to time.Time) map[string]int64 {
	out := map[string]int64{}
	for _, sp := range spans {
		start, end := sp.Start, sp.End
		if start.Before(from) {
			start = from
		}
		if end.After(to) {
			end = to
		}
		if end.After(start) {
			out[sp.State] += int64(end.Sub(start) / time.Second)
		}
	}
	return out
}

// Current liefert die letzte Messung und ihren Zeitpunkt; ok ist false vor der ersten Messung.
func (s *Store) Current() (smp Sample, at time.Time, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last, s.lastAt, s.hasLast
}
