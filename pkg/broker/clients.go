package broker

import (
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
