package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"mlc2afmqttproxy/pkg/config"
	"mlc2afmqttproxy/pkg/devices"
	"mlc2afmqttproxy/pkg/history"
)

func boolp(b bool) *bool { return &b }

// router baut die Routen wie StartServer, ohne zu lauschen.
func router(t *testing.T, sc config.ServerConf) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h, err := history.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sc.APIPrefix = "/api/v1"
	cfg := &config.Config{Server: sc}
	cfg.MQTT.Bind, cfg.MQTT.WsPort = "127.0.0.1", 1
	r := gin.New()
	registerRoutes(r, cfg, nil, Services{History: h, Devices: devices.New(""),
		Status: func() Status { return Status{State: "ok"} }}, "test", time.Now())
	return r
}

func do(r http.Handler, method, url, remote string, body string, mod func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, url, strings.NewReader(body))
	req.RemoteAddr = remote
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if mod != nil {
		mod(req)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

const lan = "192.168.1.50:40000"

func TestPublicAndProtected(t *testing.T) {
	r := router(t, config.ServerConf{Username: "mlc", Password: "geheim-im-test"})
	for url, want := range map[string]int{
		"/api/v1/health": 200, "/api/v1/status": 200, "/api/v1/history": 200,
		"/api/v1/devices": 200, "/api/v1/filters": 401, "/config": 401, "/mqtt": 401,
	} {
		if w := do(r, "GET", url, lan, "", nil); w.Code != want {
			t.Errorf("%s ohne Anmeldung: %d, soll %d", url, w.Code, want)
		}
	}
	// Basic Auth wie bis 1.1 (curl -u)
	w := do(r, "GET", "/api/v1/filters", lan, "", func(q *http.Request) { q.SetBasicAuth("mlc", "geheim-im-test") })
	if w.Code != 200 {
		t.Errorf("Basic Auth: %d", w.Code)
	}
	// Ohne UI-Header: WWW-Authenticate (curl/Browser-Dialog); mit UI-Header: keiner
	if w := do(r, "GET", "/api/v1/filters", lan, "", nil); w.Header().Get("WWW-Authenticate") == "" {
		t.Error("WWW-Authenticate fehlt")
	}
	if w := do(r, "GET", "/api/v1/filters", lan, "", func(q *http.Request) { q.Header.Set(uiHeader, "1") }); w.Header().Get("WWW-Authenticate") != "" {
		t.Error("UI-Anfrage bekommt WWW-Authenticate (Browser-Dialog)")
	}
}

func TestLoginSession(t *testing.T) {
	r := router(t, config.ServerConf{Username: "mlc", Password: "geheim-im-test"})
	if w := do(r, "POST", "/api/v1/login", lan, `{"password":"falsch"}`, nil); w.Code != 401 {
		t.Fatalf("falsches Passwort: %d", w.Code)
	}
	w := do(r, "POST", "/api/v1/login", lan, `{"password":"geheim-im-test"}`, nil)
	if w.Code != 200 {
		t.Fatalf("Login: %d %s", w.Code, w.Body)
	}
	ck := w.Result().Cookies()
	if len(ck) != 1 || !ck[0].HttpOnly || ck[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("Cookie = %+v", ck)
	}
	withCookie := func(q *http.Request) { q.AddCookie(ck[0]) }
	if w := do(r, "GET", "/api/v1/filters", lan, "", withCookie); w.Code != 200 {
		t.Fatalf("mit Cookie: %d", w.Code)
	}
	if w := do(r, "GET", "/api/v1/status", lan, "", withCookie); !strings.Contains(w.Body.String(), `"authorized":true`) {
		t.Fatalf("status nach Login: %s", w.Body)
	}
	do(r, "POST", "/api/v1/logout", lan, "", withCookie)
	if w := do(r, "GET", "/api/v1/filters", lan, "", withCookie); w.Code != 401 {
		t.Fatalf("nach Logout: %d", w.Code)
	}
}

func TestWithoutPasswordOnlyLocal(t *testing.T) {
	r := router(t, config.ServerConf{})
	if w := do(r, "GET", "/api/v1/filters", "127.0.0.1:5000", "", nil); w.Code != 200 {
		t.Errorf("lokal ohne Passwort: %d", w.Code)
	}
	if w := do(r, "GET", "/api/v1/filters", lan, "", nil); w.Code != 403 {
		t.Errorf("aus dem LAN ohne Passwort: %d, soll 403", w.Code)
	}
	if w := do(r, "GET", "/api/v1/history", lan, "", nil); w.Code != 200 {
		t.Errorf("Statistik aus dem LAN: %d", w.Code)
	}
	if w := do(r, "POST", "/api/v1/login", lan, `{"password":"x"}`, nil); w.Code != 409 {
		t.Errorf("Login ohne konfiguriertes Passwort: %d, soll 409", w.Code)
	}
}

func TestPublicSwitches(t *testing.T) {
	r := router(t, config.ServerConf{Username: "mlc", Password: "geheim-im-test", PublicDevices: boolp(false)})
	if w := do(r, "GET", "/api/v1/devices", lan, "", nil); w.Code != 401 {
		t.Errorf("public_devices=false: %d", w.Code)
	}
	if w := do(r, "GET", "/api/v1/history", lan, "", nil); w.Code != 200 {
		t.Errorf("Statistik weiter öffentlich: %d", w.Code)
	}
	r = router(t, config.ServerConf{Username: "mlc", Password: "geheim-im-test", PublicStats: boolp(false)})
	for _, url := range []string{"/api/v1/history", "/api/v1/stats", "/api/v1/devices"} {
		if w := do(r, "GET", url, lan, "", nil); w.Code != 401 {
			t.Errorf("public_stats=false %s: %d", url, w.Code)
		}
	}
	w := do(r, "GET", "/api/v1/status", lan, "", nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), `"state":"ok"`) {
		t.Errorf("status ohne Anmeldung: nur Login-Hinweis, kein Betriebszustand: %d %s", w.Code, w.Body)
	}
}

func TestStripUserinfo(t *testing.T) {
	for in, want := range map[string]string{
		"tcp://user:pw@host:1883": "tcp://host:1883",
		"tcp://host:1883":         "tcp://host:1883",
		"http://h/p@x":            "http://h/p@x",
		"":                        "",
	} {
		if got := stripUserinfo(in); got != want {
			t.Errorf("%q → %q, soll %q", in, got, want)
		}
	}
}
