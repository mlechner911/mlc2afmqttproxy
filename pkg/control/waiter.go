package control

import (
	"encoding/json"
	"strings"
	"sync"
)

// Waiters warten auf die Zustandsmeldung eines Geräts nach einem Befehl. Gefüttert werden sie mit
// jeder Meldung <base>/<gerät> — vom eigenen Broker (Direktbetrieb) oder vom z2m-Broker.
type Waiters struct {
	prefix string // "<base>/"

	mu   sync.Mutex
	byID map[string][]*waiter // friendly_name → offene Wartende
}

type waiter struct {
	want  map[string]any
	done  chan map[string]any // erhält den gemeldeten Zustand, sobald er passt
	last  map[string]any      // zuletzt gemeldeter Zustand (auch wenn er nicht passte)
	fired bool
}

// NewWaiters legt die Warteliste für das z2m-Basis-Topic an.
func NewWaiters(base string) *Waiters {
	return &Waiters{prefix: strings.TrimSuffix(base, "/") + "/", byID: map[string][]*waiter{}}
}

// Add meldet einen Wartenden für das Gerät name an. done liefert den Zustand, sobald eine
// Meldung die gewünschten Felder trägt; cancel räumt ab (Timeout) und liefert den letzten Zustand.
func (w *Waiters) Add(name string, want map[string]any) (done <-chan map[string]any, cancel func() map[string]any) {
	wt := &waiter{want: want, done: make(chan map[string]any, 1)}
	w.mu.Lock()
	w.byID[name] = append(w.byID[name], wt)
	w.mu.Unlock()
	return wt.done, func() map[string]any {
		w.mu.Lock()
		defer w.mu.Unlock()
		list := w.byID[name]
		for i, x := range list {
			if x == wt {
				w.byID[name] = append(list[:i], list[i+1:]...)
				break
			}
		}
		if len(w.byID[name]) == 0 {
			delete(w.byID, name)
		}
		return wt.last
	}
}

// Observe verarbeitet eine Meldung vom Broker (Signatur wie broker.Observer, ohne Zeit).
func (w *Waiters) Observe(topic string, payload []byte) {
	name, ok := strings.CutPrefix(topic, w.prefix)
	if !ok {
		return
	}
	w.mu.Lock()
	list := w.byID[name]
	w.mu.Unlock()
	if len(list) == 0 {
		return
	}
	var state map[string]any
	if json.Unmarshal(payload, &state) != nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, wt := range w.byID[name] {
		wt.last = state
		if !wt.fired && Matches(wt.want, state) {
			wt.fired = true
			wt.done <- state
		}
	}
}
