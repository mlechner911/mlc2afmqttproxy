package web

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"mlc2afmqttproxy/pkg/history"
)

// maxDetailWindow begrenzt /history/detail auf die Aufbewahrung der 10-Minuten-Abschnitte.
const maxDetailWindow = history.DefaultSlotDays * 24 * time.Hour

// HistoryResponse ist die Übersicht: Tageswerte, Summen je Zeitraum, letzte Unterbrechungen.
type HistoryResponse struct {
	Days    int              `json:"days" example:"30"`
	Periods []history.Period `json:"periods"`
	Daily   []history.Day    `json:"daily"`
	// Interruptions sind die Zeiträume ohne Weiterleitung im Fenster, neueste zuerst (max. 200).
	Interruptions []history.Span `json:"interruptions"`
}

// HistoryDetailResponse sind die 10-Minuten-Abschnitte und Zustandsintervalle eines Fensters.
type HistoryDetailResponse struct {
	From        time.Time      `json:"from"`
	To          time.Time      `json:"to"`
	SlotSeconds int            `json:"slot_seconds" example:"600"`
	Slots       []history.Slot `json:"slots"`
	Spans       []history.Span `json:"spans"`
}

// GetHistoryHandler liefert die Verlaufsübersicht.
// @Summary Verlauf (Tageswerte und Zeiträume)
// @Description Tageswerte der letzten `days` Tage, Summen für 1/7/30/365 Tage und die Unterbrechungen der Weiterleitung.
// @Tags History
// @Produce json
// @Param days query int false "Tage (1–400, Standard 30)"
// @Success 200 {object} HistoryResponse
// @Failure 400 {object} map[string]string
// @Router /api/v1/history [get]
func GetHistoryHandler(h *history.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		days, err := strconv.Atoi(c.DefaultQuery("days", "30"))
		if err != nil || days < 1 || days > history.DefaultRetentionDays {
			c.JSON(http.StatusBadRequest, gin.H{"error": "days muss zwischen 1 und 400 liegen"})
			return
		}
		now := time.Now()
		c.JSON(http.StatusOK, HistoryResponse{
			Days:          days,
			Periods:       h.Summary(now),
			Daily:         orEmpty(h.Days(days, now)),
			Interruptions: orEmpty(h.Interruptions(now.AddDate(0, 0, -days), 200)),
		})
	}
}

// GetHistoryDetailHandler liefert die 10-Minuten-Abschnitte eines Fensters.
// @Summary Verlauf im Detail (10-Minuten-Abschnitte)
// @Description Abschnitte und Zustandsintervalle in [from, to). Standard: die letzten 24 Stunden; höchstens 31 Tage.
// @Tags History
// @Produce json
// @Param from query string false "Beginn (RFC3339)"
// @Param to query string false "Ende (RFC3339)"
// @Success 200 {object} HistoryDetailResponse
// @Failure 400 {object} map[string]string
// @Router /api/v1/history/detail [get]
func GetHistoryDetailHandler(h *history.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		to, err := parseTime(c.Query("to"), time.Now())
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "to: " + err.Error()})
			return
		}
		from, err := parseTime(c.Query("from"), to.Add(-24*time.Hour))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "from: " + err.Error()})
			return
		}
		if !from.Before(to) || to.Sub(from) > maxDetailWindow {
			c.JSON(http.StatusBadRequest, gin.H{"error": "from muss vor to liegen, höchstens 31 Tage"})
			return
		}
		c.JSON(http.StatusOK, HistoryDetailResponse{
			From: from, To: to, SlotSeconds: int(history.SlotLength / time.Second),
			Slots: orEmpty(h.Slots(from, to)),
			Spans: orEmpty(h.Spans(from, to)),
		})
	}
}

func parseTime(v string, def time.Time) (time.Time, error) {
	if v == "" {
		return def, nil
	}
	return time.Parse(time.RFC3339, v)
}

// orEmpty macht aus nil eine leere Liste, damit das JSON [] statt null enthält.
func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
