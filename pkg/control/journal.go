package control

import (
	"sync"
	"time"
)

// Entry ist ein Befehl im Protokoll.
type Entry struct {
	Time   time.Time      `json:"time"`
	Tenant string         `json:"tenant"`
	IEEE   string         `json:"ieee"`
	Device string         `json:"device,omitempty"`
	ID     string         `json:"id"`
	Fields map[string]any `json:"fields,omitempty"`
	Result string         `json:"result"`
	Reason string         `json:"reason,omitempty"`
}

// journalSize: so viele Befehle hält das Protokoll im Speicher (das vollständige steht im Log).
const journalSize = 200

// Journal hält die letzten Befehle und zählt Ergebnisse seit dem Start.
type Journal struct {
	mu      sync.Mutex
	entries []Entry
	counts  map[string]uint64 // Ergebnis bzw. „rejected: <Grund>“
}

// NewJournal legt ein leeres Protokoll an.
func NewJournal() *Journal { return &Journal{counts: map[string]uint64{}} }

// Add protokolliert einen Befehl.
func (j *Journal) Add(e Entry) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.entries = append(j.entries, e)
	if len(j.entries) > journalSize {
		j.entries = j.entries[len(j.entries)-journalSize:]
	}
	j.counts[e.Result]++
	if e.Reason != "" && e.Result == "rejected" {
		j.counts["rejected: "+e.Reason]++
	}
}

// Recent liefert die letzten Befehle, neueste zuerst.
func (j *Journal) Recent() []Entry {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]Entry, len(j.entries))
	for i, e := range j.entries {
		out[len(out)-1-i] = e
	}
	return out
}

// Counts liefert die Zähler seit dem Start.
func (j *Journal) Counts() map[string]uint64 {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make(map[string]uint64, len(j.counts))
	for k, v := range j.counts {
		out[k] = v
	}
	return out
}
