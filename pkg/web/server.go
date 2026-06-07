// Package web stellt den Diagnose-Webserver bereit, welcher das Svelte-Frontend ausliefert,
// WebSockets an den lokalen Mochi-Broker proxyed und Status-APIs bereitstellt.
package web

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/gin-contrib/static"
	"mlc2afmqttproxy/pkg/storage"
)

// StatsResponse repräsentiert das Antwort-Dokument des Puffer-Statistik API-Endpunkts.
type StatsResponse struct {
	// Die aktuelle Anzahl der in BadgerDB zwischengespeicherten Nachrichten.
	BufferCount int `json:"buffer_count"`
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

// StartServer initialisiert und startet den Diagnose-Webserver.
// Registriert Middleware für statische Dateien, leitet WebSocket-Verbindungen
// an den lokalen Mochi-MQTT Broker weiter und registriert die JSON-APIs.
func StartServer(port int, store *storage.Store, version string) error {
	r := gin.Default()

	// Statische Dateien ausliefern (Svelte Frontend aus dem dist/ Ordner)
	r.Use(static.Serve("/", static.LocalFile("./frontend/dist", false)))

	// WebSocket-Verbindungen (für das Live-Dashboard) transparent an den Mochi Broker tunneln (Port 1885)
	wsTarget, _ := url.Parse("http://localhost:1885")
	wsProxy := httputil.NewSingleHostReverseProxy(wsTarget)
	r.GET("/mqtt", func(c *gin.Context) {
		wsProxy.ServeHTTP(c.Writer, c.Request)
	})

	// APIs registrieren
	r.GET("/api/v1/stats", GetStatsHandler(store))
	r.GET("/api/v1/health", GetHealthHandler(version))

	address := fmt.Sprintf(":%d", port)
	return r.Run(address)
}

