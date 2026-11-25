package web

import (
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"mlc2afmqttproxy/pkg/devices"
	"mlc2afmqttproxy/pkg/history"
)

// Gerätestatus in der Übersicht.
const (
	DeviceOK      = "ok"      // sendet im üblichen Takt
	DeviceSilent  = "silent"  // deutlich länger ruhig als üblich
	DeviceOffline = "offline" // zigbee2mqtt meldet es als nicht erreichbar
	DeviceUnknown = "unknown" // seit Beginn der Aufzeichnung keine Nachricht
)

// minSilence ist die kürzeste Ruhe, ab der ein Gerät als still gilt — auch bei Geräten, die
// sonst im Minutentakt senden.
const minSilence = time.Hour

// DeviceInfo ist ein Gerät mit seinen Nachrichtenzahlen und seinem Status.
type DeviceInfo struct {
	devices.Device
	Messages history.DeviceTotals `json:"messages"`
	// AvgIntervalS ist der mittlere Abstand zwischen zwei Nachrichten in den letzten 7 Tagen
	// (nur Laufzeit des Proxys); 0 = zu wenige Nachrichten für eine Aussage.
	AvgIntervalS int64  `json:"avg_interval_s"`
	Status       string `json:"status" example:"ok"`
}

// DevicesResponse ist die Geräteübersicht.
type DevicesResponse struct {
	Devices []DeviceInfo `json:"devices"`
}

// DeviceHistoryResponse sind die Nachrichten eines Geräts je Tag oder je Stunde.
type DeviceHistoryResponse struct {
	Name string `json:"name"`
	// Resolution: "day" oder "hour".
	Resolution string          `json:"resolution" example:"day"`
	Points     []history.Point `json:"points"`
}

// GetDevicesHandler liefert alle Zigbee-Geräte mit Nachrichtenzahlen und Status.
// @Summary Zigbee-Geräte
// @Description Geräte aus zigbee2mqtt mit Nachrichten heute/7/30 Tage, mittlerem Sendeabstand und Status (ok, silent, offline, unknown).
// @Tags Devices
// @Produce json
// @Success 200 {object} DevicesResponse
// @Router /api/v1/devices [get]
func GetDevicesHandler(t *devices.Tracker, h *history.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		now := time.Now()
		totals := h.DeviceTotals(now)
		ran := runSeconds(h.Summary(now))
		list := t.List()
		out := make([]DeviceInfo, 0, len(list))
		for _, d := range list {
			info := DeviceInfo{Device: d, Messages: totals[d.Name]}
			if info.Messages.Week >= 3 && ran > 0 {
				info.AvgIntervalS = ran / info.Messages.Week
			}
			info.Status = deviceStatus(info, now)
			out = append(out, info)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
		c.JSON(http.StatusOK, DevicesResponse{Devices: out})
	}
}

// runSeconds ist die Laufzeit des Proxys in den letzten 7 Tagen (alle Zustände).
func runSeconds(periods []history.Period) int64 {
	var n int64
	for _, p := range periods {
		if p.Days == 7 {
			for _, s := range p.Seconds {
				n += s
			}
		}
	}
	return n
}

func deviceStatus(d DeviceInfo, now time.Time) string {
	switch {
	case d.Availability == "offline":
		return DeviceOffline
	case d.LastSeen.IsZero():
		return DeviceUnknown
	case d.AvgIntervalS > 0 && now.Sub(d.LastSeen) > max(3*time.Duration(d.AvgIntervalS)*time.Second, minSilence):
		return DeviceSilent
	default:
		return DeviceOK
	}
}

// GetDeviceHistoryHandler liefert die Nachrichten eines Geräts je Tag (days) oder je Stunde
// (from/to).
// @Summary Verlauf eines Zigbee-Geräts
// @Description Ohne from: Tageswerte der letzten `days` Tage (1–400, Standard 30). Mit from (RFC3339, to optional): Stundenwerte, höchstens 31 Tage.
// @Tags Devices
// @Produce json
// @Param name query string true "Gerätename (friendly_name)"
// @Param days query int false "Tage"
// @Param from query string false "Beginn (RFC3339) — Stundenwerte"
// @Param to query string false "Ende (RFC3339)"
// @Success 200 {object} DeviceHistoryResponse
// @Failure 400 {object} map[string]string
// @Router /api/v1/devices/history [get]
func GetDeviceHistoryHandler(h *history.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.Query("name")
		if name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "name fehlt"})
			return
		}
		if c.Query("from") != "" {
			deviceHours(c, h, name)
			return
		}
		days, err := strconv.Atoi(c.DefaultQuery("days", "30"))
		if err != nil || days < 1 || days > history.DefaultRetentionDays {
			c.JSON(http.StatusBadRequest, gin.H{"error": "days muss zwischen 1 und 400 liegen"})
			return
		}
		c.JSON(http.StatusOK, DeviceHistoryResponse{Name: name, Resolution: "day",
			Points: orEmpty(h.DeviceDays(name, days, time.Now()))})
	}
}

func deviceHours(c *gin.Context, h *history.Store, name string) {
	from, err := time.Parse(time.RFC3339, c.Query("from"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "from: " + err.Error()})
		return
	}
	to, err := parseTime(c.Query("to"), time.Now())
	if err != nil || !from.Before(to) || to.Sub(from) > maxDetailWindow {
		c.JSON(http.StatusBadRequest, gin.H{"error": "to ungültig: muss nach from liegen, höchstens 31 Tage"})
		return
	}
	c.JSON(http.StatusOK, DeviceHistoryResponse{Name: name, Resolution: "hour",
		Points: orEmpty(h.DeviceHours(name, from, to))})
}
