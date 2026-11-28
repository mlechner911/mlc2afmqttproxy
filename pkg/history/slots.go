package history

import "time"

// Slot ist, was in einem 10-Minuten-Abschnitt geschah.
type Slot struct {
	Start time.Time `json:"start"`
	Counters
	// BufferMax ist der höchste gemessene Pufferstand im Abschnitt.
	BufferMax int64 `json:"buffer_max"`
	// Rates: kleinste und größte 15-s-Rate im Abschnitt.
	Rates Rates `json:"rates"`
}

// slotStart liefert den Beginn des Abschnitts, in dem at liegt.
func slotStart(at time.Time) time.Time {
	return at.Truncate(SlotLength)
}

// addSlot verbucht delta im Abschnitt von at. Ein laufender Proxy bekommt auch ohne Verkehr
// einen Abschnitt — null, nicht „fehlt“.
func (s *Store) addSlot(at time.Time, delta Counters, buffer int64, rates Rates) {
	start := slotStart(at)
	sl := s.slots[start.Unix()]
	if sl == nil {
		sl = &Slot{Start: start}
		s.slots[start.Unix()] = sl
	}
	sl.add(delta)
	sl.BufferMax = max(sl.BufferMax, buffer)
	sl.Rates.merge(rates)
	s.dirtySlots[start.In(at.Location()).Format(dateLayout)] = true
}
