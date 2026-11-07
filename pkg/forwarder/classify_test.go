package forwarder

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestClassifyPuback (P3-07): 0x10 ist Erfolg (bis 07.11.2026 als Poison GELÖSCHT), nur echte
// Formfehler sind endgültig, 0x87 und andere werden wiederholt.
func TestClassifyPuback(t *testing.T) {
	transportErr := errors.New("connection lost")
	for _, c := range []struct {
		code      byte
		err       error
		ok, perm bool
	}{
		{0x00, nil, true, false},
		{0x10, nil, true, false},
		{0x00, transportErr, false, false},
		{0x87, errors.New("not authorized"), false, false},
		{0x97, errors.New("quota"), false, false},
		{0x80, errors.New("unspecified"), false, false},
		{0x90, errors.New("topic invalid"), false, true},
		{0x95, errors.New("too large"), false, true},
		{0x99, errors.New("payload format"), false, true},
	} {
		err := ClassifyPuback(c.code, c.err)
		var pe *PermanentError
		if (err == nil) != c.ok || errors.As(err, &pe) != c.perm {
			t.Errorf("0x%02x: err=%v, want ok=%v perm=%v", c.code, err, c.ok, c.perm)
		}
	}
}

func TestClassifyHTTPStatus(t *testing.T) {
	for st, perm := range map[int]bool{400: true, 413: true, 415: true, 422: true, 401: false, 403: false, 404: false, 429: false, 500: false, 503: false} {
		var pe *PermanentError
		if got := errors.As(ClassifyHTTPStatus(st), &pe); got != perm {
			t.Errorf("http %d: endgültig=%v, want %v", st, got, perm)
		}
	}
}

// TestHTTPInvalidJSONIsPermanent: ungültiges JSON wird nicht still als „weitergeleitet" verbucht.
func TestHTTPInvalidJSONIsPermanent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()
	f := NewHTTPForwarder(srv.URL, "x")
	var pe *PermanentError
	if err := f.Send("zigbee2mqtt/a", []byte("{kaputt"), time.Now()); !errors.As(err, &pe) {
		t.Errorf("ungültiges JSON: %v, want PermanentError", err)
	}
}
