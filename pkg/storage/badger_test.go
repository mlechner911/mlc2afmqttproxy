package storage

import (
	"errors"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"
)

// schluesselWieBroker: so baute pkg/broker den Schlüssel bis 07.11.2026 (Uhrzeit, RFC3339Nano) —
// für den Übergang alter Einträge.
func schluesselWieBroker(t time.Time) []byte { return []byte(t.UTC().Format(time.RFC3339Nano)) }

func oeffnen(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := InitBadger(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// abarbeiten: wie der Worker — ältesten Eintrag lesen, löschen; liefert die Werte in Abarbeitungsfolge.
func abarbeiten(t *testing.T, s *Store) []string {
	t.Helper()
	var out []string
	for i := 0; i < 1000; i++ {
		k, v, err := s.PeekFirst()
		if errors.Is(err, badger.ErrKeyNotFound) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, string(v))
		if err := s.Delete(k); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("Puffer leert sich nicht")
	return nil
}

// Der Puffer ist das Versprechen „kein Datenverlust": Was vor einem Neustart (Stromausfall mit
// USV-Ende, Update) gepuffert war, ist danach noch da, wird gezählt und in Reihenfolge geliefert.
func TestPufferUeberstehtNeustart(t *testing.T) {
	dir := t.TempDir()
	s := oeffnen(t, dir)
	for _, v := range []string{"a", "b", "c"} {
		if _, err := s.Append([]byte(v)); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	s = oeffnen(t, dir)
	defer s.Close()
	if n, _ := s.GetSize(); n != 3 {
		t.Fatalf("nach Neustart %d Einträge, erwartet 3", n)
	}
	if got := abarbeiten(t, s); len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Errorf("nach Neustart %v, erwartet [a b c]", got)
	}
}

// Nach einem Neustart zählt die laufende Nummer weiter — ein neuer Eintrag überschreibt keinen,
// der noch im Puffer liegt, und kommt nach ihm.
func TestNummerGehtNachNeustartWeiter(t *testing.T) {
	dir := t.TempDir()
	s := oeffnen(t, dir)
	for _, v := range []string{"a", "b"} {
		if _, err := s.Append([]byte(v)); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	s = oeffnen(t, dir)
	defer s.Close()
	if _, err := s.Append([]byte("c")); err != nil {
		t.Fatal(err)
	}
	if got := abarbeiten(t, s); len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("nach Neustart %v, erwartet [a b c]", got)
	}
}

// Was später ankommt, wird auch dann geliefert, wenn der Vorgänger schon abgearbeitet ist (Lese-
// Marke steht auf ihm). Mit der Uhrzeit als Schlüssel blieb so ein Eintrag bis zum Neustart liegen:
// „…00.12Z" sortiert als Text vor „…00.1Z" (RFC3339Nano lässt Nullen am Ende weg), und stellt NTP
// die Uhr zurück, liegt alles Neue „vor" der Marke. Die laufende Nummer hängt an keiner Uhr.
func TestSpaeterEintragBleibtNichtLiegen(t *testing.T) {
	s := oeffnen(t, t.TempDir())
	defer s.Close()
	if _, err := s.Append([]byte("erster")); err != nil {
		t.Fatal(err)
	}
	if got := abarbeiten(t, s); len(got) != 1 {
		t.Fatalf("erster: %v", got)
	}
	if _, err := s.Append([]byte("zweiter")); err != nil {
		t.Fatal(err)
	}
	if got := abarbeiten(t, s); len(got) != 1 || got[0] != "zweiter" {
		t.Errorf("der später angekommene Eintrag wurde nicht geliefert: %v", got)
	}
}

// Viele Einträge kurz hintereinander (auch mit gleicher Uhrzeit, grobe Uhr, mehrere Geräte
// zugleich): keiner geht verloren, die Reihenfolge ist die Ankunft, der Zähler stimmt.
func TestKeinEintragGehtVerloren(t *testing.T) {
	s := oeffnen(t, t.TempDir())
	defer s.Close()
	var soll []string
	for i := 0; i < 300; i++ {
		v := string(rune('A'+i%26)) + string(rune('0'+i/26%10)) + string(rune('0'+i/260))
		soll = append(soll, v)
		if _, err := s.Append([]byte(v)); err != nil {
			t.Fatal(err)
		}
	}
	if n, _ := s.GetSize(); n != 300 {
		t.Errorf("Zähler %d, erwartet 300", n)
	}
	got := abarbeiten(t, s)
	if len(got) != 300 {
		t.Fatalf("%d von 300 geliefert", len(got))
	}
	for i := range soll {
		if got[i] != soll[i] {
			t.Fatalf("Reihenfolge ab %d: %q statt %q", i, got[i], soll[i])
		}
	}
}

// Übergang: Einträge, die beim Update noch im alten Format (Uhrzeit als Schlüssel) im Puffer
// liegen, sind älter und gehen vor den neuen hinaus.
func TestAlteEintraegeZuerst(t *testing.T) {
	s := oeffnen(t, t.TempDir())
	defer s.Close()
	if err := s.Push(schluesselWieBroker(time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)), []byte("alt")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append([]byte("neu")); err != nil {
		t.Fatal(err)
	}
	if got := abarbeiten(t, s); len(got) != 2 || got[0] != "alt" || got[1] != "neu" {
		t.Errorf("%v, erwartet [alt neu]", got)
	}
}
