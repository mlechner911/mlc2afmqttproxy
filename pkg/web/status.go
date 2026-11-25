package web

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"mlc2afmqttproxy/pkg/config"
	"mlc2afmqttproxy/pkg/metrics"
)

// Status ist der Kurzstatus für die Kopfzeile und für Kunden: läuft der Proxy, leitet er weiter?
// Ohne Ziel-Adressen oder Zugangsdaten — der Endpunkt ist öffentlich.
type Status struct {
	Version string `json:"version"`
	UptimeS int64  `json:"uptime_s"`
	Mode    string `json:"mode" example:"mqtt"`
	// State: ok, upstream_down, paused, buffer_full (wie im Verlauf).
	State             string `json:"state" example:"ok"`
	BufferPending     int64  `json:"buffer_pending"`
	UpstreamConnected bool   `json:"upstream_connected"`
	ZigbeeConnected   bool   `json:"zigbee_connected"`
	// Alter in Sekunden; -1 = seit dem Start noch nie.
	LastForwardAgeS int64 `json:"last_forward_age_s"`
	LastReceiveAgeS int64 `json:"last_receive_age_s"`
}

// StatusResponse ist der Kurzstatus plus das, was die Oberfläche über die Anmeldung wissen muss.
type StatusResponse struct {
	Status
	// LoginConfigured: Es gibt ein Passwort; die Live-Ansicht lässt sich anmelden.
	LoginConfigured bool `json:"login_configured"`
	// Authorized: Diese Anfrage darf den geschützten Teil sehen (angemeldet oder lokal ohne Passwort).
	Authorized    bool `json:"authorized"`
	DevicesPublic bool `json:"devices_public"`
}

// GetStatusHandler liefert den Kurzstatus.
// @Summary Kurzstatus
// @Description Läuft der Proxy, leitet er weiter? Zustand, Puffer, Verbindungen, Alter der letzten Weiterleitung. Öffentlich, wenn server.public_stats (Standard).
// @Tags Status
// @Produce json
// @Success 200 {object} StatusResponse
// @Router /api/v1/status [get]
func GetStatusHandler(status func() Status, a *auth) gin.HandlerFunc {
	return func(c *gin.Context) {
		resp := StatusResponse{
			LoginConfigured: a.conf.HasLogin(),
			Authorized:      a.allowed(c),
			DevicesPublic:   a.conf.DevicesPublic(),
		}
		// Ohne öffentliche Statistik verrät /status Unangemeldeten nur, dass eine Anmeldung nötig ist.
		if a.conf.StatsPublic() || resp.Authorized {
			resp.Status = status()
		}
		c.JSON(http.StatusOK, resp)
	}
}

// Rule ist eine Filterregel mit ihren Treffern seit dem Start.
type Rule struct {
	// Kind: allow (Topic muss mit einem dieser Präfixe beginnen) oder ignore.
	Kind   string `json:"kind" example:"ignore"`
	Prefix string `json:"prefix,omitempty"`
	Hits   uint64 `json:"hits"`
}

// RouteInfo ist eine zusätzliche Route ohne Zugangsdaten.
type RouteInfo struct {
	Name     string   `json:"name"`
	Upstream string   `json:"upstream"`
	Devices  []string `json:"devices,omitempty"`
}

// FiltersResponse zeigt, was der Proxy verwirft oder umleitet.
type FiltersResponse struct {
	Rules             []Rule      `json:"rules"`
	DedupIntervalMs   int         `json:"dedup_interval_ms"`
	DedupIgnoreKeys   []string    `json:"dedup_ignore_keys"`
	DeduplicatedTotal uint64      `json:"deduplicated_total"`
	Routes            []RouteInfo `json:"routes"`
	Target            string      `json:"target"`
}

// GetFiltersHandler liefert Filterregeln, Deduplizierung und Routen.
// @Summary Filter und Routen
// @Description Topic-Filter mit Treffern je Regel seit dem Start, Deduplizierung, zusätzliche Routen. Nur angemeldet.
// @Tags Status
// @Produce json
// @Success 200 {object} FiltersResponse
// @Failure 401 {object} map[string]string
// @Router /api/v1/filters [get]
func GetFiltersHandler(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		hits := metrics.FilterHits()
		resp := FiltersResponse{
			Rules:             []Rule{},
			DedupIntervalMs:   cfg.MQTT.DeduplicateIntervalMs,
			DedupIgnoreKeys:   orEmpty(cfg.MQTT.DeduplicateIgnoreKeys),
			DeduplicatedTotal: metrics.GetStats().MessagesDeduplicated,
			Routes:            []RouteInfo{},
			Target:            target(cfg),
		}
		if f := cfg.MQTT.Filter; f != nil {
			for _, p := range f.AllowedPrefixes {
				resp.Rules = append(resp.Rules, Rule{Kind: "allow", Prefix: p})
			}
			if len(f.AllowedPrefixes) > 0 {
				// Treffer der Positivliste: Topics, die zu KEINEM Präfix passten.
				resp.Rules = append(resp.Rules, Rule{Kind: "not_allowed", Hits: hits["allow"]})
			}
			for _, p := range f.IgnoredPrefixes {
				resp.Rules = append(resp.Rules, Rule{Kind: "ignore", Prefix: p, Hits: hits["ignore:"+p]})
			}
		}
		for _, r := range cfg.Routes {
			up := r.Upstream
			if up == "" {
				up = r.HTTP.Endpoint
			}
			resp.Routes = append(resp.Routes, RouteInfo{Name: r.Name, Upstream: stripUserinfo(up), Devices: r.Devices})
		}
		c.JSON(http.StatusOK, resp)
	}
}

func target(cfg *config.Config) string {
	if cfg.Mode == "http" {
		return stripUserinfo(cfg.HTTP.Endpoint)
	}
	return stripUserinfo(cfg.MQTT.Upstream)
}

// stripUserinfo entfernt user:pass@ aus einer URL, falls jemand Zugangsdaten hineinschrieb.
func stripUserinfo(u string) string {
	scheme, rest, ok := strings.Cut(u, "://")
	if !ok {
		return u
	}
	if at := strings.LastIndex(rest, "@"); at >= 0 && at < strings.IndexAny(rest+"/", "/") {
		rest = rest[at+1:]
	}
	return scheme + "://" + rest
}
