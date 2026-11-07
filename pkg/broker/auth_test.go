package broker

import (
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"

	"mlc2afmqttproxy/pkg/config"
	"mlc2afmqttproxy/pkg/storage"
)

var testUsers = []config.LocalUser{{Username: "z2m", Password: "test-passwort-lokal"}}

// TestLocalAuthHook (P3-01): von Loopback ohne Anmeldung, von außen nur mit gültigem Konto;
// unlesbare Gegenstelle zählt als außen.
func TestLocalAuthHook(t *testing.T) {
	h := NewLocalAuthHook(testUsers)
	conn := func(remote, user, pass string) bool {
		cl := &mqtt.Client{Net: mqtt.ClientConnection{Remote: remote}}
		var pk packets.Packet
		pk.Connect.Username, pk.Connect.Password = []byte(user), []byte(pass)
		return h.OnConnectAuthenticate(cl, pk)
	}
	cases := []struct {
		remote, user, pass string
		want               bool
	}{
		{"127.0.0.1:50000", "", "", true},
		{"[::1]:50000", "", "", true},
		{"192.168.1.50:50000", "", "", false},
		{"192.168.1.50:50000", "z2m", "falsch", false},
		{"192.168.1.50:50000", "fremd", "test-passwort-lokal", false},
		{"192.168.1.50:50000", "z2m", "test-passwort-lokal", true},
		{"", "", "", false},            // unbekannte Gegenstelle
		{"kaputt", "", "", false},      // unlesbar
		{"10.0.0.1:1", "z2m", "", false}, // leeres Passwort
	}
	for _, c := range cases {
		if got := conn(c.remote, c.user, c.pass); got != c.want {
			t.Errorf("remote=%q user=%q: %v, want %v", c.remote, c.user, got, c.want)
		}
	}
	if NewLocalAuthHook(nil).OnConnectAuthenticate(&mqtt.Client{Net: mqtt.ClientConnection{Remote: "10.0.0.1:1"}}, packets.Packet{}) {
		t.Error("ohne Konten von außen angenommen")
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// lanIP liefert eine eigene Nicht-Loopback-IPv4-Adresse (für „von außen"), sonst "".
func lanIP() string {
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && !n.IP.IsLoopback() && n.IP.To4() != nil {
			return n.IP.String()
		}
	}
	return ""
}

func startBroker(t *testing.T, bind string, users []config.LocalUser) (*storage.Store, int) {
	t.Helper()
	st, err := storage.InitBadger(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	port, ws := freePort(t), freePort(t)
	srv, err := StartLocalBroker(bind, users, port, ws, st, nil, 0, nil, 1024)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close(); st.Close() })
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := net.Dial("tcp", net.JoinHostPort(bind, fmt.Sprint(port))); err == nil {
			c.Close()
			return st, port
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("Broker lauscht nicht")
	return nil, 0
}

func connect(addr, user, pass string) (paho.Client, error) {
	o := paho.NewClientOptions().AddBroker("tcp://" + addr).SetClientID(fmt.Sprintf("t-%d", time.Now().UnixNano())).
		SetConnectTimeout(2 * time.Second).SetAutoReconnect(false).SetConnectRetry(false)
	if user != "" {
		o.SetUsername(user).SetPassword(pass)
	}
	c := paho.NewClient(o)
	tok := c.Connect()
	if !tok.WaitTimeout(3 * time.Second) {
		return nil, fmt.Errorf("timeout")
	}
	return c, tok.Error()
}

// TestBrokerDefaultNotReachableFromLAN: Mit bind 127.0.0.1 ist der Broker über die eigene
// LAN-Adresse nicht erreichbar.
func TestBrokerDefaultNotReachableFromLAN(t *testing.T) {
	ip := lanIP()
	if ip == "" {
		t.Skip("keine Nicht-Loopback-Adresse auf diesem Rechner")
	}
	_, port := startBroker(t, "127.0.0.1", nil)
	if c, err := net.DialTimeout("tcp", net.JoinHostPort(ip, fmt.Sprint(port)), time.Second); err == nil {
		c.Close()
		t.Fatalf("Broker über %s erreichbar, obwohl bind 127.0.0.1", ip)
	}
}

// TestBrokerOpenBindRequiresLogin: Auf allen Schnittstellen lauschend kommt von der LAN-Adresse
// nur ein Konto hinein — anonym oder falsch nicht. Von Loopback weiter ohne Anmeldung.
func TestBrokerOpenBindRequiresLogin(t *testing.T) {
	ip := lanIP()
	if ip == "" {
		t.Skip("keine Nicht-Loopback-Adresse auf diesem Rechner")
	}
	_, port := startBroker(t, "0.0.0.0", testUsers)
	lan := net.JoinHostPort(ip, fmt.Sprint(port))
	if c, err := connect(lan, "", ""); err == nil {
		c.Disconnect(10)
		t.Error("anonym über die LAN-Adresse angenommen")
	}
	if c, err := connect(lan, "z2m", "falsch"); err == nil {
		c.Disconnect(10)
		t.Error("falsches Passwort angenommen")
	}
	c, err := connect(lan, "z2m", "test-passwort-lokal")
	if err != nil {
		t.Fatalf("gültiges Konto abgewiesen: %v", err)
	}
	c.Disconnect(10)
	c, err = connect(net.JoinHostPort("127.0.0.1", fmt.Sprint(port)), "", "")
	if err != nil {
		t.Fatalf("Loopback ohne Anmeldung abgewiesen: %v", err)
	}
	c.Disconnect(10)
}

// TestBrokerRejectsOversizedPacket (P3-08): Eine Nachricht über der Grenze wird nicht angenommen
// und nicht gepuffert; eine normale danach schon.
func TestBrokerRejectsOversizedPacket(t *testing.T) {
	st, port := startBroker(t, "127.0.0.1", nil)
	addr := net.JoinHostPort("127.0.0.1", fmt.Sprint(port))
	big, err := connect(addr, "", "")
	if err != nil {
		t.Fatal(err)
	}
	payload := `{"temperature":1,"x":"` + strings.Repeat("a", MaxLocalPacketSize+1) + `"}`
	big.Publish("zigbee2mqtt/gross", 1, false, payload).WaitTimeout(2 * time.Second)
	big.Disconnect(10)

	ok, err := connect(addr, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer ok.Disconnect(10)
	if tok := ok.Publish("zigbee2mqtt/klein", 1, false, `{"temperature":4.2}`); !tok.WaitTimeout(2*time.Second) || tok.Error() != nil {
		t.Fatalf("normale Nachricht: %v", tok.Error())
	}
	time.Sleep(100 * time.Millisecond)
	if n, _ := st.GetSize(); n != 1 {
		t.Fatalf("gepuffert: %d, want 1 (nur die normale Nachricht)", n)
	}
}
