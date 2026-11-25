package metrics

import (
	"sync"
	"sync/atomic"
)

// Treffer je Filterregel seit dem Start. Schlüssel: „allow“ (Topic in keinem allowed_prefix)
// oder „ignore:<prefix>“. Die Zahl der Regeln ist durch die Konfiguration begrenzt.
var (
	filterMu   sync.RWMutex
	filterHits = map[string]*uint64{}
	bufferFull atomic.Bool
)

// IncFiltered zählt eine von der Filterregel rule verworfene Nachricht.
func IncFiltered(rule string) {
	atomic.AddUint64(&messagesFiltered, 1)
	filterMu.RLock()
	c, ok := filterHits[rule]
	filterMu.RUnlock()
	if !ok {
		filterMu.Lock()
		if c, ok = filterHits[rule]; !ok {
			c = new(uint64)
			filterHits[rule] = c
		}
		filterMu.Unlock()
	}
	atomic.AddUint64(c, 1)
}

// FilterHits liefert die Treffer je Filterregel seit dem Start.
func FilterHits() map[string]uint64 {
	filterMu.RLock()
	defer filterMu.RUnlock()
	out := make(map[string]uint64, len(filterHits))
	for k, c := range filterHits {
		out[k] = atomic.LoadUint64(c)
	}
	return out
}

// SetBufferFull merkt, ob der Puffer sein Limit erreicht hat (Tail-Drop aktiv).
func SetBufferFull(full bool) { bufferFull.Store(full) }

// BufferFull meldet, ob der Puffer gerade voll ist und neue Nachrichten verworfen werden.
func BufferFull() bool { return bufferFull.Load() }
