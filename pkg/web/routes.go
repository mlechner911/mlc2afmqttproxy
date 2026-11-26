package web

import (
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"mlc2afmqttproxy/pkg/config"
	"mlc2afmqttproxy/pkg/storage"
)

// registerRoutes teilt die Endpunkte in öffentlich und geschützt (seit 1.2.0).
//
// Öffentlich (bei server.public_stats, Standard): Seite, health, status, stats, history und —
// bei server.public_devices — devices. Der Kunde sieht ohne Anmeldung, ob der Proxy läuft und
// weiterleitet. Geschützt: Live-Ansicht (/mqtt: alle MQTT-Inhalte), /config (Ziel-Adresse) und
// /filters. Bis 1.1 lag alles hinter einer gemeinsamen Basic Auth.
func registerRoutes(r *gin.Engine, cfg *config.Config, store *storage.Store, svc Services, version string, startTime time.Time) {
	a := &auth{conf: cfg.Server, sessions: newSessions()}
	api := cfg.Server.APIPrefix
	open := r.Group("")
	stats := r.Group("")
	if !cfg.Server.StatsPublic() {
		stats.Use(a.require())
	}
	devices := stats
	if !cfg.Server.DevicesPublic() {
		devices = r.Group("", a.require())
	}
	protected := r.Group("", a.require())

	open.GET(api+"/health", GetHealthHandler(version))
	open.POST(api+"/login", a.login)
	open.POST(api+"/logout", a.logout)
	if svc.Status != nil {
		open.GET(api+"/status", GetStatusHandler(svc.Status, a)) // meldet selbst, ob angemeldet
	}

	stats.GET(api+"/stats", GetStatsHandler(store))
	if svc.History != nil {
		stats.GET(api+"/history", GetHistoryHandler(svc.History))
		stats.GET(api+"/history/detail", GetHistoryDetailHandler(svc.History))
		if svc.Devices != nil {
			devices.GET(api+"/devices", GetDevicesHandler(svc.Devices, svc.History))
			devices.GET(api+"/devices/history", GetDeviceHistoryHandler(svc.History))
		}
	}

	protected.GET("/config", GetConfigHandler(cfg, startTime))
	protected.GET(api+"/filters", GetFiltersHandler(cfg))
	if svc.Control != nil {
		protected.GET(api+"/control", GetControlHandler(svc.Control))
	}
	protected.GET("/mqtt", liveTunnel(cfg))
}

// liveTunnel leitet WebSocket-Verbindungen (Live-Ansicht) an den WebSocket-Listener des lokalen
// Brokers weiter (mqtt.bind, Standard 127.0.0.1). Die Verbindung kommt dort von Loopback an und
// braucht keine Broker-Anmeldung — geschützt wird sie hier: Anmeldung (requireAuth) und
// Origin-Prüfung gegen Cross-Site-WebSocket-Hijacking (P3-02).
func liveTunnel(cfg *config.Config) gin.HandlerFunc {
	wsHost := cfg.MQTT.Bind
	if wsHost == "" || wsHost == "0.0.0.0" || wsHost == "::" {
		wsHost = "127.0.0.1"
	}
	wsTarget, _ := url.Parse("http://" + net.JoinHostPort(wsHost, strconv.Itoa(cfg.MQTT.WsPort)))
	wsProxy := httputil.NewSingleHostReverseProxy(wsTarget)
	return func(c *gin.Context) {
		if !sameOrigin(c.Request) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		wsProxy.ServeHTTP(c.Writer, c.Request)
	}
}
