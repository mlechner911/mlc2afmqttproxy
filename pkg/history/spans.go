package history

import "time"

// Span ist ein Zeitraum in einem Zustand (State*), sekundengenau.
type Span struct {
	State string    `json:"state" example:"upstream_down"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	// Open: Der Zeitraum dauert noch an (End ist die letzte Messung).
	Open bool `json:"open,omitempty"`
}

// recordSpan führt die Zustandsintervalle fort. Gleicher Zustand ohne Lücke verlängert das
// offene Intervall; ein Zustandswechsel oder eine Lücke (> maxGap) beginnt ein neues — die Zeit
// dazwischen bleibt eine Lücke (Proxy lief nicht).
func (s *Store) recordSpan(state string, at time.Time) {
	if state == "" {
		state = StateOK
	}
	at = at.Truncate(time.Second)
	if n := len(s.spans); n > 0 {
		cur := &s.spans[n-1]
		if cur.Open && s.hasLast && at.Sub(s.lastAt) <= maxGap {
			cur.End = at
			if cur.State == state {
				s.markSpan(*cur)
				return
			}
			cur.Open = false
			s.markSpan(*cur)
		} else if cur.Open {
			cur.Open = false // Lücke: endet bei der letzten Messung
			s.markSpan(*cur)
		}
	}
	sp := Span{State: state, Start: at, End: at, Open: true}
	s.spans = append(s.spans, sp)
	s.markSpan(sp)
}

func (s *Store) markSpan(sp Span) {
	s.dirtySpans[sp.Start.Format(monthLayout)] = true
}

// closeOpenSpans schließt beim Laden Intervalle, die der vorige Lauf offen hinterließ: Sie enden
// bei ihrer letzten Messung.
func (s *Store) closeOpenSpans() {
	for i := range s.spans {
		if s.spans[i].Open {
			s.spans[i].Open = false
			s.markSpan(s.spans[i])
		}
	}
}
