package forwarder

import "time"

// Forwarder definiert die Schnittstelle für den Upstream-Versand von Telemetrie-Daten.
// Es gibt Implementierungen für MQTT und HTTP.
type Forwarder interface {
	// Send übermittelt eine Nachricht an den Upstream-Dienst
	Send(topic string, payload []byte, timestamp time.Time) error
	
	// IsConnected prüft, ob die Verbindung zum Upstream besteht
	IsConnected() bool
	
	// Connect stellt die Verbindung her (bzw. initialisiert den Client)
	Connect() error
	
	// Close beendet die Verbindung sauber
	Close()
}
