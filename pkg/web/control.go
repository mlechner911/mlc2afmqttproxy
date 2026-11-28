package web

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"mlc2afmqttproxy/pkg/control"
)

// ControlTenant ist ein Mandant des Rückwegs mit seiner Freigabe.
type ControlTenant struct {
	Name      string `json:"name" example:"main"`
	Subscribe string `json:"subscribe" example:"cmd/zb/#"`
	// Allow: operate (Bedienfelder + Liste) | writable (alles Setzbare) | list (nur die Liste).
	Allow string `json:"allow" example:"operate"`
	// Listed: Felder je Gerät aus der Konfiguration (IEEE → Felder).
	Listed map[string][]string `json:"listed"`
	// Devices: nur diese Geräte (Route); leer = alle, die zigbee2mqtt kennt.
	Devices []string `json:"devices,omitempty"`
}

// ControlResponse zeigt den Rückweg: Freigaben, Zähler seit dem Start, letzte Befehle.
type ControlResponse struct {
	Enabled bool `json:"enabled"`
	// Direct: Befehle über den eigenen Broker; sonst über control.broker (z. B. Mosquitto).
	Direct bool `json:"direct"`
	// DefaultFields: Bedienfelder im Modus operate (state, brightness, …).
	DefaultFields []string          `json:"default_fields"`
	Tenants       []ControlTenant   `json:"tenants"`
	Counts        map[string]uint64 `json:"counts"`
	Recent        []control.Entry   `json:"recent"`
}

// GetControlHandler liefert den Stand des Rückwegs.
// @Summary Rückweg (Befehle aus dem Upstream)
// @Description Freigaben je Upstream (Haupt und Routen), Ergebnisse seit dem Start, die letzten 200 Befehle. Nur angemeldet.
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
