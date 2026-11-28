package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 25, 10, 0, 0, 0, time.Local)

func open(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func smp(received, forwarded, buffer int64, state string) Sample {
	return Sample{Totals: Counters{Received: received, Forwarded: forwarded}, Buffer: buffer, State: state}
}

func TestRecordDeltas(t *testing.T) {
	tests := []struct {
		name    string
		samples []Sample
		want    int64 // empfangen am Tag
	}{
		{"erste Messung zählt ganz", []Sample{smp(5, 0, 0, StateOK)}, 5},
		{"Differenzen", []Sample{smp(5, 0, 0, StateOK), smp(8, 0, 0, StateOK), smp(20, 0, 0, StateOK)}, 20},
		{"Neustart des Zählers", []Sample{smp(10, 0, 0, StateOK), smp(3, 0, 0, StateOK)}, 13},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := open(t, t.TempDir())
			for i, x := range tt.samples {
				s.Record(x, t0.Add(time.Duration(i)*SampleInterval))
			}
			days := s.Days(1, t0)
			if len(days) != 1 || days[0].Received != tt.want {
				t.Fatalf("Tage = %+v, empfangen soll %d", days, tt.want)
			}
		})
	}
}

func TestSlotsAndBufferMax(t *testing.T) {
	s := open(t, t.TempDir())
	s.Record(smp(1, 1, 4, StateOK), t0)
	s.Record(smp(2, 1, 9, StateOK), t0.Add(5*time.Minute))
	s.Record(smp(3, 3, 2, StateOK), t0.Add(11*time.Minute)) // nächster Abschnitt
	slots := s.Slots(t0, t0.Add(time.Hour))
	if len(slots) != 2 {
		t.Fatalf("Abschnitte = %d, soll 2", len(slots))
	}
	if slots[0].Received != 2 || slots[0].BufferMax != 9 || slots[1].Forwarded != 2 {
		t.Fatalf("Abschnitte falsch: %+v", slots)
	}
}

func TestSpans(t *testing.T) {
	s := open(t, t.TempDir())
	at := t0
	step := func(state string) {
		s.Record(smp(0, 0, 0, state), at)
		at = at.Add(SampleInterval)
	}
	step(StateOK)
	step(StateOK)
	step(StateUpstreamDown)
	step(StateUpstreamDown)
	step(StateOK)
	at = at.Add(10 * time.Minute) // Proxy lief nicht
	step(StateOK)

	got := s.Spans(t0, at)
	want := []struct {
		state string
		secs  int
	}{{StateOK, 30}, {StateUpstreamDown, 30}, {StateOK, 0}, {StateOK, 0}}
	if len(got) != len(want) {
		t.Fatalf("Intervalle = %+v", got)
	}
	for i, w := range want {
		if got[i].State != w.state || int(got[i].End.Sub(got[i].Start).Seconds()) != w.secs {
			t.Errorf("Intervall %d = %s %v, soll %s %ds", i, got[i].State, got[i].End.Sub(got[i].Start), w.state, w.secs)
		}
	}
	if in := s.Interruptions(t0, 10); len(in) != 1 || in[0].State != StateUpstreamDown {
		t.Errorf("Unterbrechungen = %+v", in)
	}
	sum := s.Summary(at)
	if sum[0].Seconds[StateUpstreamDown] != 30 {
		t.Errorf("Sekunden gestört = %v", sum[0].Seconds)
	}
}

func TestFlushAndReload(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	s.Record(smp(7, 5, 3, StateOK), t0)
	s.Record(smp(9, 6, 1, StatePaused), t0.Add(SampleInterval))
	if err := s.Flush(t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	r := open(t, dir)
	days := r.Days(1, t0)
	if len(days) != 1 || days[0].Received != 9 || days[0].Forwarded != 6 || days[0].BufferMax != 3 {
		t.Fatalf("nach Neuladen: %+v", days)
	}
	if len(r.Slots(t0.Add(-time.Hour), t0.Add(time.Hour))) != 1 {
		t.Fatal("Abschnitt fehlt nach Neuladen")
	}
	spans := r.Spans(t0, t0.Add(time.Hour))
	if len(spans) != 2 || spans[1].Open {
		t.Fatalf("Intervalle nach Neuladen (offene müssen geschlossen sein): %+v", spans)
	}
}

func TestBrokenFileIsMovedAside(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, daysDir, "2026-11.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("{kaputt"), 0o640); err != nil {
		t.Fatal(err)
	}
	open(t, dir)
	if _, err := os.Stat(p + ".broken"); err != nil {
		t.Fatalf("kaputte Datei nicht beiseitegelegt: %v", err)
	}
}

func TestPrune(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	old := t0.AddDate(0, 0, -(DefaultSlotDays + 2))
	s.Record(smp(1, 0, 0, StateOK), old)
	s.Record(smp(2, 0, 0, StateOK), t0)
	if err := s.Flush(t0); err != nil {
		t.Fatal(err)
	}
	if n := len(s.Slots(old.Add(-time.Hour), t0.Add(time.Hour))); n != 1 {
		t.Fatalf("alte Abschnitte nicht entfernt: %d", n)
	}
	if _, err := os.Stat(filepath.Join(dir, slotsDir, old.Format(dateLayout)+".json")); !os.IsNotExist(err) {
		t.Fatalf("Datei des alten Abschnitts noch da: %v", err)
	}
	if len(s.Days(DefaultSlotDays+5, t0)) != 2 {
		t.Fatal("Tageswerte (400 Tage) dürfen nach 31 Tagen nicht weg sein")
	}
}

func TestDevices(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	rec := func(at time.Time, c map[string]int64) {
		s.Record(Sample{State: StateOK, Devices: c}, at)
	}
	rec(t0.AddDate(0, 0, -10), map[string]int64{"a": 5})
	rec(t0.AddDate(0, 0, -3), map[string]int64{"a": 2, "b": 1})
	rec(t0, map[string]int64{"a": 1})
	rec(t0.Add(70*time.Minute), map[string]int64{"a": 4})

	tot := s.DeviceTotals(t0.Add(2 * time.Hour))
	if tot["a"] != (DeviceTotals{Today: 5, Week: 7, Month: 12}) || tot["b"].Week != 1 {
		t.Fatalf("Summen = %+v", tot)
	}
	hours := s.DeviceHours("a", t0, t0.Add(3*time.Hour))
	if len(hours) != 2 || hours[0].Count != 1 || hours[1].Count != 4 {
		t.Fatalf("Stunden = %+v", hours)
	}
	if err := s.Flush(t0.Add(2 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	r := open(t, dir)
	days := r.DeviceDays("b", 30, t0)
	if len(days) != 3 || days[1].Count != 1 || days[0].Count != 0 {
		t.Fatalf("Tage nach Neuladen = %+v", days)
	}
}

// TestRates: kleinste und größte 15-s-Rate je Abschnitt, Tag und Zeitraum. Die erste Messung (ganzer
// Stand) und eine Lücke zählen nicht; nach dem Schreiben und Laden sind die Werte noch da.
func TestRates(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	at := t0
	rec := func(received, forwarded int64, step time.Duration) {
		at = at.Add(step)
		s.Record(smp(received, forwarded, 0, StateOK), at)
	}
	rec(1000, 1000, 0)               // erste Messung: kein Fenster
	rec(1030, 1015, SampleInterval)  // 2 /s und 1 /s
	rec(1030, 1015, SampleInterval)  // 0 /s
	rec(1120, 1105, SampleInterval)  // 6 /s
	rec(1420, 1405, 10*time.Minute)  // Lücke: zählt nicht
	want := Rates{Received: Rate{Min: 0, Max: 6, Samples: 3}, Forwarded: Rate{Min: 0, Max: 6, Samples: 3}}

	if d := s.Days(1, at)[0].Rates; d != want {
		t.Fatalf("Tag: %+v, soll %+v", d, want)
	}
	if sl := s.Slots(t0, t0.Add(SlotLength))[0].Rates; sl != want {
		t.Fatalf("Abschnitt: %+v, soll %+v", sl, want)
	}
	if p := s.Summary(at)[0].Rates; p != want {
		t.Fatalf("Zeitraum: %+v, soll %+v", p, want)
	}
	if err := s.Flush(at); err != nil {
		t.Fatal(err)
	}
	if d := open(t, dir).Days(1, at)[0].Rates; d != want {
		t.Fatalf("nach Laden: %+v, soll %+v", d, want)
	}
}
