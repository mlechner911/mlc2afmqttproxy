package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"mlc2afmqttproxy/pkg/devices"
	"mlc2afmqttproxy/pkg/history"
)

func TestDeviceStatus(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name string
		d    DeviceInfo
		want string
	}{
		{"offline laut z2m", DeviceInfo{Device: devices.Device{Availability: "offline", LastSeen: now}}, DeviceOffline},
		{"nie gesehen", DeviceInfo{}, DeviceUnknown},
		{"im Takt", DeviceInfo{Device: devices.Device{LastSeen: now.Add(-2 * time.Hour)}, AvgIntervalS: 3600}, DeviceOK},
		{"still (> 3× Takt)", DeviceInfo{Device: devices.Device{LastSeen: now.Add(-4 * time.Hour)}, AvgIntervalS: 3600}, DeviceSilent},
		{"Minutentakt, aber erst 30 min ruhig", DeviceInfo{Device: devices.Device{LastSeen: now.Add(-30 * time.Minute)}, AvgIntervalS: 60}, DeviceOK},
		{"zu wenige Nachrichten", DeviceInfo{Device: devices.Device{LastSeen: now.Add(-48 * time.Hour)}}, DeviceOK},
	}
	for _, tt := range tests {
		if got := deviceStatus(tt.d, now); got != tt.want {
			t.Errorf("%s: %s, soll %s", tt.name, got, tt.want)
		}
	}
}

func TestDeviceEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, err := history.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tr := devices.New("")
	now := time.Now()
	tr.Observe("zigbee2mqtt/sensor", []byte(`{"battery":50}`), now)
	h.Record(history.Sample{State: history.StateOK, Devices: tr.Drain()}, now)

	r := gin.New()
	r.GET("/devices", GetDevicesHandler(tr, h))
	r.GET("/devices/history", GetDeviceHistoryHandler(h))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/devices", nil))
	var resp DevicesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Devices) != 1 || resp.Devices[0].Messages.Today != 1 || resp.Devices[0].Status != DeviceOK {
		t.Fatalf("Geräte = %s", w.Body)
	}
	for url, code := range map[string]int{
		"/devices/history?name=sensor":          200,
		"/devices/history?name=sensor&days=500": 400,
		"/devices/history":                      400,
		"/devices/history?name=sensor&from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z": 200,
		"/devices/history?name=sensor&from=2026-01-01T00:00:00Z&to=2026-03-01T00:00:00Z": 400,
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, url, nil))
		if w.Code != code {
			t.Errorf("%s: %d, soll %d", url, w.Code, code)
		}
	}
}
