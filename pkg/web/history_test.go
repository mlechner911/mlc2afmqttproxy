package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"mlc2afmqttproxy/pkg/history"
)

func TestHistoryEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, err := history.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	h.Record(history.Sample{Totals: history.Counters{Received: 4, Forwarded: 3}, State: history.StateOK}, now.Add(-time.Second)) // dicht beieinander: kein Tageswechsel dazwischen
	h.Record(history.Sample{Totals: history.Counters{Received: 6, Forwarded: 3}, State: history.StateUpstreamDown}, now)

	r := gin.New()
	r.GET("/history", GetHistoryHandler(h))
	r.GET("/history/detail", GetHistoryDetailHandler(h))

	tests := []struct {
		url  string
		code int
	}{
		{"/history", 200},
		{"/history?days=365", 200},
		{"/history?days=0", 400},
		{"/history?days=401", 400},
		{"/history/detail", 200},
		{"/history/detail?from=2026-01-01T00:00:00Z&to=2026-03-01T00:00:00Z", 400}, // > 31 Tage
		{"/history/detail?from=kaputt", 400},
	}
	for _, tt := range tests {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tt.url, nil))
		if w.Code != tt.code {
			t.Errorf("%s: Status %d, soll %d (%s)", tt.url, w.Code, tt.code, w.Body)
		}
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/history?days=7", nil))
	var resp HistoryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Periods) != 4 || resp.Periods[0].Received != 6 || len(resp.Interruptions) != 1 {
		t.Fatalf("Antwort: %+v", resp)
	}
}
