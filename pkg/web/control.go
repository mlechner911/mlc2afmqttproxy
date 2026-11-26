package web

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"mlc2afmqttproxy/pkg/control"
)

// ControlTenant ist ein Mandant des Rückwegs mit seiner Freigabe.
type ControlTenant struct {
	Name     string              `json:"name" example:"main"`
	Customer string              `json:"customer"`
	Allowed  map[string][]string `json:"allowed"` // IEEE → Felder
}

// ControlResponse zeigt den Rückweg: Freigaben, Zähler seit dem Start, letzte Befehle.
type ControlResponse struct {
	Enabled bool `json:"enabled"`
	// Direct: Befehle über den eigenen Broker; sonst über control.broker (z. B. Mosquitto).
	Direct  bool              `json:"direct"`
	Tenants []ControlTenant   `json:"tenants"`
	Counts  map[string]uint64 `json:"counts"`
	Recent  []control.Entry   `json:"recent"`
}

// GetControlHandler liefert den Stand des Rückwegs.
// @Summary Rückweg (Befehle aus dem Monitor)
// @Description Freigegebene Geräte je Mandant, Ergebnisse seit dem Start, die letzten 200 Befehle. Nur angemeldet.
// @Tags Control
// @Produce json
// @Success 200 {object} ControlResponse
// @Failure 401 {object} map[string]string
// @Router /api/v1/control [get]
func GetControlHandler(state func() *ControlResponse) gin.HandlerFunc {
	return func(c *gin.Context) {
		if s := state(); s != nil {
			c.JSON(http.StatusOK, s)
			return
		}
		c.JSON(http.StatusOK, ControlResponse{Tenants: []ControlTenant{}, Counts: map[string]uint64{}, Recent: []control.Entry{}})
	}
}
