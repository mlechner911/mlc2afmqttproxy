package broker

import (
	"strings"
	"sync/atomic"
	"time"

	mqtt "github.com/mochi-mqtt/server/v2"
)

// tcpListenerID ist die ID des lokalen TCP-Listeners (zigbee2mqtt bzw. mqttbridge). Das
// Dashboard kommt über den WebSocket-Listener und zählt nicht mit.
const tcpListenerID = "tcp-local"

// LocalClients zählt die verbundenen Clients am lokalen TCP-Listener — 0 heißt: die
// Funk-Software (bzw. die mqttbridge davor) ist nicht verbunden.
func LocalClients(s *mqtt.Server) int {
	if s == nil {
		return 0
	}
	n := 0
	for _, cl := range s.Clients.GetAll() {
		if cl.Net.Listener == tcpListenerID && !cl.Closed() {
			n++
		}
	}
	return n
}

// Observer erhält jede lokal empfangene Nachricht, bevor Filter und Deduplizierung greifen
// (Gerätestatistik). Muss schnell sein; er läuft im Empfangspfad des Brokers.
type Observer func(topic string, payload []byte, at time.Time)

var observer atomic.Pointer[Observer]

// SetObserver setzt den Beobachter für alle lokalen Broker dieses Prozesses (nil = keiner).
func SetObserver(o Observer) {
	if o == nil {
		observer.Store(nil)
		return
	}
	observer.Store(&o)
}

func observe(topic string, payload []byte) {
	if o := observer.Load(); o != nil {
		(*o)(topic, payload, time.Now())
	}
}

// Befehle sind keine Messwerte: Was der Proxy selbst einspeist (InlineClient, Rückweg im
// Direktbetrieb) und was an zigbee2mqtt gerichtet ist (<base>/…/set, …/get, <base>/bridge/request/…),
// wird nicht gepuffert und nicht nach oben weitergeleitet. Bis 26.11.2026 gingen z. B. die
// Schaltbefehle von Home Assistant als „Messwerte“ an den Monitor.
var (
	commandBase   atomic.Pointer[string]
	inlineEnabled atomic.Bool
)

// SetCommandBase setzt das Basis-Topic von zigbee2mqtt für die Erkennung von Befehlen.
func SetCommandBase(base string) {
	b := strings.TrimSuffix(base, "/") + "/"
	commandBase.Store(&b)
}

// EnableInlineClient schaltet den eingebauten Client des Brokers ein (vor StartLocalBroker) — nur
// für den Rückweg im Direktbetrieb (control ohne broker).
func EnableInlineClient() { inlineEnabled.Store(true) }

// isCommand: Nachricht des eigenen Clients oder ein Befehl an zigbee2mqtt.
func isCommand(cl *mqtt.Client, topic string) bool {
	if cl != nil && cl.Net.Inline {
		return true
	}
	bp := commandBase.Load()
	if bp == nil {
		return false
	}
	rest, ok := strings.CutPrefix(topic, *bp)
	if !ok {
		return false
	}
	if strings.HasPrefix(rest, "bridge/request/") {
		return true
	}
	for _, seg := range strings.Split(rest, "/")[1:] {
		if seg == "set" || seg == "get" {
			return true
		}
	}
	return false
}
