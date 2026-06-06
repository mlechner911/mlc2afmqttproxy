package web

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"mlc2afmqttproxy/pkg/storage"
)

// StartServer startet den Diagnose-Webserver
func StartServer(port int, store *storage.Store, version string) error {
	r := gin.Default()

	// Einfaches HTML Template laden
	r.LoadHTMLGlob("templates/*")

	r.GET("/", func(c *gin.Context) {
		count, _ := store.GetSize()
		recent, _ := store.GetRecent(10)

		c.HTML(http.StatusOK, "index.html", gin.H{
			"title": "Zigbee Gateway Proxy Dashboard",
			"buffer_count": count,
			"recent": recent,
		})
	})

	r.GET("/api/v1/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"version": version,
		})
	})

	address := fmt.Sprintf(":%d", port)
	return r.Run(address)
}
