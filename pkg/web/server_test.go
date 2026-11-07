package web

import (
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"mlc2afmqttproxy/pkg/config"
	"mlc2afmqttproxy/pkg/storage"
)

func startTestServer(t *testing.T, user, pass string) string {
	t.Helper()
	st, err := storage.InitBadger(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	cfg := &config.Config{}
	cfg.Server = config.ServerConf{Enable: true, Host: "127.0.0.1", Port: port, APIPrefix: "/api/v1", Username: user, Password: pass}
	cfg.MQTT.Bind, cfg.MQTT.WsPort = "127.0.0.1", 1 // Tunnelziel nicht erreichbar — hier geht es um die Prüfung davor
	srv := StartServer(cfg, st, "test", time.Now())
	t.Cleanup(func() { _ = srv.Close(); st.Close() })
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	for i := 0; i < 100; i++ {
		if r, err := http.Get(base + "/api/v1/health"); err == nil {
			r.Body.Close()
			return base
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("Webserver startet nicht")
	return ""
}

func get(t *testing.T, url string, hdr map[string]string, user, pass string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	return r
}

// TestDashboardNoCORSAndHeaders (P3-10): kein Access-Control-Allow-Origin mehr (bis 07.11.2026 "*"),
// Sicherheits-Header gesetzt.
func TestDashboardNoCORSAndHeaders(t *testing.T) {
	base := startTestServer(t, "", "")
	r := get(t, base+"/api/v1/stats", map[string]string{"Origin": "http://evil.example"}, "", "")
	if v := r.Header.Get("Access-Control-Allow-Origin"); v != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want keinen", v)
	}
	for _, h := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options"} {
		if r.Header.Get(h) == "" {
			t.Errorf("Header %s fehlt", h)
		}
	}
}

// TestDashboardTunnelChecksOrigin (P3-02): Der WebSocket-Tunnel /mqtt lehnt fremde Herkunft ab —
// sonst könnte jede Webseite im Browser eines angemeldeten Nutzers Live-Daten lesen und
// veröffentlichen (Cross-Site-WebSocket-Hijacking).
func TestDashboardTunnelChecksOrigin(t *testing.T) {
	base := startTestServer(t, "mlc", "test-passwort-dashboard")
	ws := map[string]string{"Connection": "Upgrade", "Upgrade": "websocket", "Sec-WebSocket-Version": "13", "Sec-WebSocket-Key": "dGhlIHNhbXBsZSBub25jZQ=="}
	with := func(origin string) map[string]string {
		m := map[string]string{"Origin": origin}
		for k, v := range ws {
			m[k] = v
		}
		return m
	}
	if r := get(t, base+"/mqtt", with("http://evil.example"), "mlc", "test-passwort-dashboard"); r.StatusCode != http.StatusForbidden {
		t.Errorf("fremde Herkunft: %d, want 403", r.StatusCode)
	}
	// Gleiche Herkunft kommt durch die Prüfung (Ziel hier absichtlich nicht erreichbar → 502).
	if r := get(t, base+"/mqtt", with(base), "mlc", "test-passwort-dashboard"); r.StatusCode == http.StatusForbidden {
		t.Errorf("gleiche Herkunft abgewiesen")
	}
	if r := get(t, base+"/mqtt", with(base), "", ""); r.StatusCode != http.StatusUnauthorized {
		t.Errorf("ohne Anmeldung: %d, want 401", r.StatusCode)
	}
}

func TestSameOrigin(t *testing.T) {
	for _, c := range []struct {
		host, origin string
		want         bool
	}{
		{"box:8097", "", true},
		{"box:8097", "http://box:8097", true},
		{"box:8097", "https://BOX:8097", true},
		{"box:8097", "http://evil.example", false},
		{"box:8097", "http://box:8098", false},
		{"box:8097", "null", false},
	} {
		r, _ := http.NewRequest(http.MethodGet, "http://"+c.host+"/mqtt", nil)
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if got := sameOrigin(r); got != c.want {
			t.Errorf("Host %s, Origin %q: %v, want %v", c.host, c.origin, got, c.want)
		}
	}
}
