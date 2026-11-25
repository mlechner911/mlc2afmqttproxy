package broker

import mqtt "github.com/mochi-mqtt/server/v2"

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
