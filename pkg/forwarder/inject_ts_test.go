package forwarder

import (
	"encoding/json"
	"testing"
	"time"
)

// P3-11 (Sicherheitsprüfung 07.11.2026): Die Empfangszeit setzt immer die Box. Ein vom Absender
// mitgeschicktes Feld darf sie nicht ersetzen — sonst ließen sich Werte rückdatieren. Sein Wert
// bleibt als Angabe unter <Feld>_sender erhalten.
func TestInjectTimestampCannotBeBackdated(t *testing.T) {
	ts := time.UnixMilli(1794660000000)
	for _, f := range []struct {
		was, in string
		sender  any
	}{
		{"ohne Feld", `{"temperature":4.2}`, nil},
		{"rückdatiert", `{"temperature":4.2,"_ts":1000}`, float64(1000)},
	} {
		var got map[string]any
		if err := json.Unmarshal(injectTimestamp([]byte(f.in), "_ts", ts), &got); err != nil {
			t.Fatalf("%s: %v", f.was, err)
		}
		if got["_ts"] != float64(ts.UnixMilli()) {
			t.Errorf("%s: _ts = %v, erwartet die Empfangszeit %d", f.was, got["_ts"], ts.UnixMilli())
		}
		if got["_ts_sender"] != f.sender {
			t.Errorf("%s: _ts_sender = %v, erwartet %v", f.was, got["_ts_sender"], f.sender)
		}
		if got["temperature"] != 4.2 {
			t.Errorf("%s: Messwert verändert: %v", f.was, got["temperature"])
		}
	}
	// Kein JSON-Objekt: unverändert durchreichen.
	for _, in := range []string{"21.5", "kein json", `[1,2]`} {
		if out := string(injectTimestamp([]byte(in), "_ts", ts)); out != in {
			t.Errorf("%q verändert zu %q", in, out)
		}
	}
}
