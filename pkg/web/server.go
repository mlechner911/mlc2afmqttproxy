// Package web stellt den Diagnose-Webserver bereit, welcher das Svelte-Frontend ausliefert,
// WebSockets an den lokalen Mochi-Broker proxyed und Status-APIs bereitstellt.
package web

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
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

// StartServer initialisiert und startet den Diagnose-Webserver.
// Registriert Middleware für statische Dateien, leitet WebSocket-Verbindungen
// an den lokalen Mochi-MQTT Broker weiter und registriert die JSON-APIs.
func StartServer(cfg *config.Config, store *storage.Store, version string, startTime time.Time) error {
	r := gin.Default()

	// CORS Middleware: Da das API Read-Only ist, erlauben wir per Default alle Anfragen
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept")
		
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
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
	wsTarget, _ := url.Parse(fmt.Sprintf("http://localhost:%d", cfg.MQTT.WsPort))
	wsProxy := httputil.NewSingleHostReverseProxy(wsTarget)
	r.GET("/mqtt", func(c *gin.Context) {
		wsProxy.ServeHTTP(c.Writer, c.Request)
	})

	// APIs registrieren
	r.GET(cfg.Server.APIPrefix+"/stats", GetStatsHandler(store))
	r.GET(cfg.Server.APIPrefix+"/health", GetHealthHandler(version))

	address := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	return r.Run(address)
}

