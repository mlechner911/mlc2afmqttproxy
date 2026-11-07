// Package web stellt den Diagnose-Webserver bereit, welcher das Svelte-Frontend ausliefert,
// WebSockets an den lokalen Mochi-Broker proxyed und Status-APIs bereitstellt.
package web

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"
	"embed"
	"io/fs"

	"github.com/gin-gonic/gin"
	"mlc2afmqttproxy/pkg/metrics"
	"mlc2afmqttproxy/pkg/storage"
	"mlc2afmqttproxy/pkg/config"
)

// StatsResponse repräsentiert das Antwort-Dokument des Puffer-Statistik API-Endpunkts.
type StatsResponse struct {
	// Die aktuelle Anzahl der in BadgerDB zwischengespeicherten Nachrichten.
	BufferCount int `json:"buffer_count"`
	metrics.Stats
}

// HealthResponse repräsentiert das Antwort-Dokument des Health-Check API-Endpunkts.
type HealthResponse struct {
	// Status der Anwendung (normalerweise "ok").
	Status string `json:"status"`
	// Die aktuelle Version der Proxy-Anwendung.
	Version string `json:"version"`
}

// GetStatsHandler gibt einen Gin-Handler zurück, der die Anzahl der gepufferten Nachrichten liefert.
// @Summary Puffer-Statistik abrufen
// @Description Gibt die Anzahl der aktuell in BadgerDB zwischengespeicherten Nachrichten zurück.
// @Tags Stats
// @Produce json
// @Success 200 {object} StatsResponse "Erfolgreiche Antwort"
// @Router /api/v1/stats [get]
func GetStatsHandler(store *storage.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		count, _ := store.GetSize()
		c.JSON(http.StatusOK, StatsResponse{
			BufferCount: count,
			Stats:       metrics.GetStats(),
		})
	}
}

// GetHealthHandler gibt einen Gin-Handler für den Service-Status zurück.
// @Summary Health-Check abrufen
// @Description Zeigt an, ob der Proxy läuft, und gibt die aktuelle Version aus.
// @Tags Health
// @Produce json
// @Success 200 {object} HealthResponse "Erfolgreiche Antwort"
// @Router /api/v1/health [get]
func GetHealthHandler(version string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, HealthResponse{
			Status:  "ok",
			Version: version,
		})
	}
}

// ConfigResponse repräsentiert die Konfigurationsdaten für das Frontend.
type ConfigResponse struct {
	// Das konfigurierte Präfix für die Diagnose-REST-API (z.B. /api/v1).
	APIPrefix string `json:"api_prefix"`
	// Wie lange läuft der Server schon
	UptimeSeconds float64 `json:"uptime_seconds"`
	// Welcher Forwarding-Modus ist aktiv
	Mode string `json:"mode"`
	// Wohin funkt der Proxy
	Target string `json:"target"`
}

// GetConfigHandler liefert die APIPrefix-Konfiguration als JSON.
func GetConfigHandler(cfg *config.Config, startTime time.Time) gin.HandlerFunc {
	return func(c *gin.Context) {
		uptime := time.Since(startTime).Seconds()
		target := cfg.MQTT.Upstream
		if cfg.Mode == "http" {
			target = cfg.HTTP.Endpoint
		}
		c.JSON(http.StatusOK, ConfigResponse{
			APIPrefix: cfg.Server.APIPrefix,
			UptimeSeconds: uptime,
			Mode: cfg.Mode,
			Target: target,
		})
	}
}

//go:embed static/*
var staticFS embed.FS

// StartServer initialisiert den Diagnose-Webserver.
// Registriert Middleware für statische Dateien, leitet WebSocket-Verbindungen
// an den lokalen Mochi-MQTT Broker weiter und registriert die JSON-APIs.
// Startet den Server in einer Goroutine und gibt das *http.Server Objekt für Graceful Shutdown zurück.
func StartServer(cfg *config.Config, store *storage.Store, version string, startTime time.Time) *http.Server {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	_ = r.SetTrustedProxies(nil) // keine X-Forwarded-For-Angaben übernehmen

	// HTTP Basic Auth für Dashboard und API. Ohne Passwort lauscht der Server nur auf 127.0.0.1
	// (config.applySecureDefaults) — bis 07.11.2026 auf allen Schnittstellen ohne Anmeldung.
	if cfg.Server.Username != "" && cfg.Server.Password != "" {
		r.Use(gin.BasicAuth(gin.Accounts{
			cfg.Server.Username: cfg.Server.Password,
		}))
	}

	// Sicherheits-Header. Kein CORS mehr: Bis 07.11.2026 stand hier Access-Control-Allow-Origin "*"
	// — jede fremde Webseite im Browser eines Nutzers im LAN konnte Konfiguration, Statistik und
	// Puffer abfragen (Sicherheitsprüfung P3-10). Das Dashboard läuft auf derselben Herkunft.
	r.Use(func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'self'; connect-src 'self' ws: wss:; img-src 'self' data:; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		c.Next()
	})

	// Statische Dateien ausliefern (Go embedFS)
	_, err := fs.Sub(staticFS, "static")
	if err != nil {
		// Fallback: Wenn 'static' Ordner leer/nicht gefunden wird, ignoriere den Fehler, 
		// um Tests/Builds ohne index.html durchlaufen zu lassen.
		fmt.Printf("Warnung: static Ordner nicht gefunden in embedFS: %v\n", err)
	} else {
		// Serve assets directory
		assetFS, _ := fs.Sub(staticFS, "static/assets")
		r.StaticFS("/assets", http.FS(assetFS))
		
		// Serve index.html on root
		r.GET("/", func(c *gin.Context) {
			indexFile, err := staticFS.ReadFile("static/index.html")
			if err != nil {
				c.String(http.StatusNotFound, "index.html not found")
				return
			}
			c.Data(http.StatusOK, "text/html; charset=utf-8", indexFile)
		})
	}

	// API-Konfigurationsendpunkt für das Svelte-Frontend bereitstellen
	r.GET("/config", GetConfigHandler(cfg, startTime))

	// WebSocket-Verbindungen (für das Live-Dashboard) transparent an den Mochi Broker tunneln (Port 1885)
	// Ziel ist der WebSocket-Listener des lokalen Brokers (mqtt.bind, Standard 127.0.0.1). Die
	// Verbindung kommt dort von Loopback an und braucht keine Broker-Anmeldung — geschützt wird sie
	// hier: Dashboard-Anmeldung und Origin-Prüfung (gegen Cross-Site-WebSocket-Hijacking, P3-02).
	wsHost := cfg.MQTT.Bind
	if wsHost == "" || wsHost == "0.0.0.0" || wsHost == "::" {
		wsHost = "127.0.0.1"
	}
	wsTarget, _ := url.Parse("http://" + net.JoinHostPort(wsHost, strconv.Itoa(cfg.MQTT.WsPort)))
	wsProxy := httputil.NewSingleHostReverseProxy(wsTarget)
	r.GET("/mqtt", func(c *gin.Context) {
		if !sameOrigin(c.Request) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		wsProxy.ServeHTTP(c.Writer, c.Request)
	})

	// APIs registrieren
	r.GET(cfg.Server.APIPrefix+"/stats", GetStatsHandler(store))
	r.GET(cfg.Server.APIPrefix+"/health", GetHealthHandler(version))

	address := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:    address,
		Handler: r,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("Fehler beim Webserver: %v\n", err)
		}
	}()

	return srv
}

// sameOrigin lässt den WebSocket-Tunnel nur für Seiten derselben Herkunft zu: Origin fehlt (kein
// Browser) oder sein Host gleicht dem angefragten Host. Browser senden beim WebSocket-Aufbau
// immer einen Origin — eine fremde Seite trägt dort ihre eigene Adresse.
func sameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, err := url.Parse(o)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}
