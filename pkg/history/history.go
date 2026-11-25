// Package history hält fest, was der Proxy über die Zeit getan hat — nur auf dieser Box:
//
//   - Tageswerte (lokale Zeit): empfangen, gefiltert, weitergeleitet, Fehler …, eine JSON-Datei
//     je Monat, aufbewahrt DefaultRetentionDays (400 Tage);
//   - 10-Minuten-Abschnitte (Slots) mit denselben Zählern und dem höchsten Pufferstand, eine
//     Datei je Tag, aufbewahrt DefaultSlotDays (31 Tage);
//   - Zustandsintervalle (Spans): von wann bis wann weitergeleitet wurde, der Upstream fehlte,
//     die Fernsteuerung pausiert hatte oder der Puffer voll war — sekundengenau, so lange wie
//     die Tageswerte.
//
// Zeit, in der der Proxy nicht lief, ist eine Lücke — nie „ok“ und nie „gestört“.
// Vorbild: internal/history in mlc-autotunnel.
package history

import (
	"sync"
	"time"
)

const (
	// DefaultRetentionDays ist die Aufbewahrung der Tageswerte und Zustandsintervalle.
	DefaultRetentionDays = 400
	// DefaultSlotDays ist die Aufbewahrung der 10-Minuten-Abschnitte.
	DefaultSlotDays = 31
	// SlotLength ist die Länge eines Abschnitts.
	SlotLength = 10 * time.Minute
	// maxGap begrenzt die Zeit zwischen zwei Messungen, die noch als durchgehend gilt. Eine
	// längere Pause heißt: der Proxy lief nicht (oder hing) — diese Zeit bleibt eine Lücke.
	maxGap = 2 * time.Minute

	dateLayout  = "2006-01-02"
	monthLayout = "2006-01"
)

// Zustände der Weiterleitung, nach Vorrang geordnet (der erste zutreffende gilt).
const (
	StateBufferFull   = "buffer_full"   // Puffer am Limit, neue Nachrichten werden verworfen
	StateUpstreamDown = "upstream_down" // Upstream nicht erreichbar, es wird gepuffert
	StatePaused       = "paused"        // per Fernsteuerung pausiert, es wird gepuffert
	StateOK           = "ok"            // es wird weitergeleitet
)

// Counters sind die Nachrichtenzähler eines Zeitraums.
type Counters struct {
	Received     int64 `json:"received"`
	Filtered     int64 `json:"filtered"`
	Deduplicated int64 `json:"deduplicated"`
	Stored       int64 `json:"stored"`
	Forwarded    int64 `json:"forwarded"`
	Failed       int64 `json:"failed"`
	DeadLettered int64 `json:"dead_lettered"`
	Dropped      int64 `json:"dropped_buffer_full"`
}

func (c *Counters) add(o Counters) {
	c.Received += o.Received
	c.Filtered += o.Filtered
	c.Deduplicated += o.Deduplicated
	c.Stored += o.Stored
	c.Forwarded += o.Forwarded
	c.Failed += o.Failed
	c.DeadLettered += o.DeadLettered
	c.Dropped += o.Dropped
}

// since bildet die Differenz zweier Zählerstände. Ein gefallener Zähler heißt: er zählt neu
// ab null (Neustart), dann zählt der ganze neue Stand.
func since(now, prev Counters) Counters {
	d := func(n, p int64) int64 {
		if n < p {
			return n
		}
		return n - p
	}
	return Counters{
		Received:     d(now.Received, prev.Received),
		Filtered:     d(now.Filtered, prev.Filtered),
		Deduplicated: d(now.Deduplicated, prev.Deduplicated),
		Stored:       d(now.Stored, prev.Stored),
		Forwarded:    d(now.Forwarded, prev.Forwarded),
		Failed:       d(now.Failed, prev.Failed),
		DeadLettered: d(now.DeadLettered, prev.DeadLettered),
		Dropped:      d(now.Dropped, prev.Dropped),
	}
}

// Day ist, was an einem Tag (lokale Zeit) geschah.
type Day struct {
	Date string `json:"date" example:"2026-11-25"`
	Counters
	// BufferMax ist der höchste gemessene Pufferstand des Tages (Nachrichten).
	BufferMax int64 `json:"buffer_max"`
}

// Sample ist eine Messung: Zählerstände seit dem Start, Pufferstand und Zustand.
type Sample struct {
	Totals Counters
	Buffer int64
	State  string
}

// Store sammelt Tageswerte, Abschnitte und Zustandsintervalle und schreibt sie nach dir.
type Store struct {
	dir string

	mu         sync.Mutex
	days       map[string]*Day // Datum → Tag
	dirtyDays  map[string]bool // Monate („2006-01“) zum Schreiben
	slots      map[int64]*Slot // Abschnittsbeginn (Unix) → Abschnitt
	dirtySlots map[string]bool // Tage zum Schreiben
	spans      []Span          // zeitlich geordnet; das letzte kann noch offen sein
	dirtySpans map[string]bool // Monate der Span-Anfänge zum Schreiben

	last    Sample
	lastAt  time.Time
	hasLast bool
}

// Open lädt die in dir abgelegten Werte. Kaputte Dateien werden als .broken beiseitegelegt.
func Open(dir string) (*Store, error) {
	s := &Store{dir: dir, days: map[string]*Day{}, dirtyDays: map[string]bool{},
		slots: map[int64]*Slot{}, dirtySlots: map[string]bool{}, dirtySpans: map[string]bool{}}
	for _, load := range []func() error{s.loadDays, s.loadSlots, s.loadSpans} {
		if err := load(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Record verbucht, was seit der vorigen Messung geschah. Die erste Messung nach dem Start
// zählt den ganzen Stand, denn die Zähler beginnen mit dem Prozess bei null.
func (s *Store) Record(smp Sample, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delta := smp.Totals
	if s.hasLast {
		delta = since(smp.Totals, s.last.Totals)
	}
	s.recordSpan(smp.State, at)
	s.last, s.lastAt, s.hasLast = smp, at, true

	date := at.Format(dateLayout)
	d := s.days[date]
	if d == nil {
		d = &Day{Date: date}
		s.days[date] = d
	}
	d.add(delta)
	d.BufferMax = max(d.BufferMax, smp.Buffer)
	s.dirtyDays[date[:len(monthLayout)]] = true
	s.addSlot(at, delta, smp.Buffer)
}
