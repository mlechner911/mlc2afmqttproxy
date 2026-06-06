package web

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	//"fmt"

	"github.com/gin-gonic/gin"
	"github.com/gin-contrib/static"
	"mlc2afmqttproxy/pkg/storage"
)

// StartServer startet den Diagnose-Webserver
func StartServer(port int, store *storage.Store, version string) error {
	r := gin.Default()

	// Statische Dateien ausliefern (Svelte Frontend)
	r.Use(static.Serve("/", static.LocalFile("./frontend/dist", false)))

	// Proxy WebSockets to Local Mochi Broker on Port 1885
	wsTarget, _ := url.Parse("http://localhost:1885")
	wsProxy := httputil.NewSingleHostReverseProxy(wsTarget)
	r.GET("/mqtt", func(c *gin.Context) {
		wsProxy.ServeHTTP(c.Writer, c.Request)
	})

	// API für Buffer Count
	r.GET("/api/v1/stats", func(c *gin.Context) {
		count, _ := store.GetSize()
		c.JSON(http.StatusOK, gin.H{
			"buffer_count": count,
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
