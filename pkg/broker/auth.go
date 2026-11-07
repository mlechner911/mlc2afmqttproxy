package broker

import (
	"bytes"
	"crypto/subtle"
	"net"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"

	"mlc2afmqttproxy/pkg/config"
)

// LocalAuthHook entscheidet, wer sich am lokalen Broker anmelden darf.
//
// Was hier ankommt, geht mit dem echten Ingest-Token in den Nachweis eines Mandanten. Bis
// 07.11.2026 nahm der Broker jede Verbindung an (auth.AllowHook) und lauschte auf allen
// Schnittstellen: Wer das Netz der Box erreichte, konnte Messwerte fälschen und allen Verkehr
// mitlesen (Sicherheitsprüfung P3-01/P3-02).
//
// Regel: Von Loopback (Bridge, zigbee2mqtt auf der Box, Dashboard-Tunnel /mqtt — dort schützt die
// Dashboard-Anmeldung) ohne Anmeldung; von jeder anderen Adresse nur mit einem Konto aus
// mqtt.local_users. Unbekannte oder nicht lesbare Gegenstelle zählt als „von außen" (fail closed).
type LocalAuthHook struct {
	mqtt.HookBase
	users []config.LocalUser
}

// NewLocalAuthHook legt den Hook mit den Konten aus der Konfiguration an.
func NewLocalAuthHook(users []config.LocalUser) *LocalAuthHook {
	return &LocalAuthHook{users: users}
}

// ID benennt den Hook.
func (h *LocalAuthHook) ID() string { return "local-auth" }

// Provides meldet die Hook-Punkte.
func (h *LocalAuthHook) Provides(b byte) bool {
	return bytes.Contains([]byte{mqtt.OnConnectAuthenticate, mqtt.OnACLCheck}, []byte{b})
}

// OnConnectAuthenticate: Loopback frei, sonst Konto nötig.
func (h *LocalAuthHook) OnConnectAuthenticate(cl *mqtt.Client, pk packets.Packet) bool {
	if cl != nil && cl.Net.Inline {
		return true
	}
	if cl != nil && fromLoopback(cl.Net.Remote) {
		return true
	}
	return h.validUser(string(pk.Connect.Username), pk.Connect.Password)
}

// OnACLCheck: Wer angemeldet ist, darf lesen und schreiben (eine Box, ein Zweck). Feinere Rechte
// (nur z2m schreibt zigbee2mqtt/#) erst mit Rollen je Konto.
func (h *LocalAuthHook) OnACLCheck(_ *mqtt.Client, _ string, _ bool) bool { return true }

func (h *LocalAuthHook) validUser(name string, pass []byte) bool {
	if name == "" || len(pass) == 0 {
		return false
	}
	ok := false
	for _, u := range h.users {
		if u.Username == "" || u.Password == "" {
			continue
		}
		// alle Konten vergleichen (keine frühe Rückkehr) und in konstanter Zeit
		n := subtle.ConstantTimeCompare([]byte(u.Username), []byte(name))
		p := subtle.ConstantTimeCompare([]byte(u.Password), pass)
		if n&p == 1 {
			ok = true
		}
	}
	return ok
}

// fromLoopback meldet, ob die Gegenstelle („ip:port") eine Loopback-Adresse ist.
func fromLoopback(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
