package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"mlc2afmqttproxy/pkg/config"
)

// Anmeldung für den geschützten Teil (Live-Ansicht, Konfiguration, Filter).
//
// Zwei Wege: ein Session-Cookie aus POST /api/v1/login (für die Oberfläche) oder HTTP Basic Auth
// (für curl und Skripte, wie bis 1.1). Ohne konfiguriertes Passwort gibt es den geschützten Teil
// nur für Anfragen von 127.0.0.1 — hinter einem Reverse Proxy auf derselben Box kämen damit alle
// herein; dort deshalb immer ein Passwort setzen.

const (
	sessionCookie = "mlcproxy_session"
	sessionTTL    = 12 * time.Hour
	// uiHeader setzt die Oberfläche bei ihren Anfragen; dann antwortet ein 401 ohne
	// WWW-Authenticate, damit der Browser nicht seinen eigenen Anmeldedialog öffnet.
	uiHeader = "X-Proxy-UI"
	// failDelay bremst das Durchprobieren von Passwörtern.
	failDelay = time.Second
)

// sessions hält die angemeldeten Sitzungen im Speicher (ein Neustart meldet alle ab).
type sessions struct {
	mu   sync.Mutex
	byID map[string]time.Time // Token → Ablauf
}

func newSessions() *sessions { return &sessions{byID: map[string]time.Time{}} }

func (s *sessions) create() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	id := hex.EncodeToString(b)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, exp := range s.byID { // Abgelaufene bei Gelegenheit wegräumen
		if now.After(exp) {
			delete(s.byID, k)
		}
	}
	s.byID[id] = now.Add(sessionTTL)
	return id, nil
}

func (s *sessions) valid(id string) bool {
	if id == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.byID[id]
	return ok && time.Now().Before(exp)
}

func (s *sessions) drop(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byID, id)
}

// auth bündelt die Prüfung gegen die Server-Konfiguration.
type auth struct {
	conf     config.ServerConf
	sessions *sessions
	// failMu reiht Fehlversuche hintereinander: höchstens etwa einer je failDelay, auch parallel.
	failMu sync.Mutex
}

func equal(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

// allowed: angemeldet per Cookie oder Basic Auth — oder, ohne konfiguriertes Passwort, von 127.0.0.1.
func (a *auth) allowed(c *gin.Context) bool {
	if !a.conf.HasLogin() {
		return isLoopbackRemote(c.Request)
	}
	if id, err := c.Cookie(sessionCookie); err == nil && a.sessions.valid(id) {
		return true
	}
	u, p, ok := c.Request.BasicAuth()
	return ok && equal(u, a.conf.Username) && equal(p, a.conf.Password)
}

// require ist die Middleware des geschützten Teils.
func (a *auth) require() gin.HandlerFunc {
	return func(c *gin.Context) {
		if a.allowed(c) {
			c.Next()
			return
		}
		if !a.conf.HasLogin() {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "nur lokal: kein server.password gesetzt"})
			return
		}
		if c.GetHeader(uiHeader) == "" {
			c.Header("WWW-Authenticate", `Basic realm="MLC Edge Proxy"`)
		}
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Anmeldung erforderlich"})
	}
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password" binding:"required"`
}

// login prüft Benutzer und Passwort und setzt das Session-Cookie. Der Benutzer darf fehlen
// (die Oberfläche fragt nur das Passwort ab).
func (a *auth) login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password fehlt"})
		return
	}
	if !a.conf.HasLogin() {
		c.JSON(http.StatusConflict, gin.H{"error": "kein server.password gesetzt — Live-Ansicht nur lokal"})
		return
	}
	userOK := req.Username == "" || equal(req.Username, a.conf.Username)
	if !userOK || !equal(req.Password, a.conf.Password) {
		a.failMu.Lock()
		time.Sleep(failDelay)
		a.failMu.Unlock()
		c.JSON(http.StatusUnauthorized, gin.H{"error": "falsches Passwort"})
		return
	}
	id, err := a.sessions.create()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Sitzung nicht angelegt"})
		return
	}
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(sessionCookie, id, int(sessionTTL/time.Second), "/", "", c.Request.TLS != nil, true)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (a *auth) logout(c *gin.Context) {
	if id, err := c.Cookie(sessionCookie); err == nil {
		a.sessions.drop(id)
	}
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(sessionCookie, "", -1, "/", "", c.Request.TLS != nil, true)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// isLoopbackRemote: Die Anfrage kommt von dieser Box (127.0.0.0/8 oder ::1).
func isLoopbackRemote(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
