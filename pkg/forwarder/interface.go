// Package forwarder definiert die Schnittstellen und konkreten Implementierungen
// zur Übermittlung von Telemetriedaten an Upstream-Dienste (HTTP oder MQTT).
package forwarder

import "time"

// Forwarder definiert das einheitliche Verhalten für den Upstream-Versand von Telemetrie-Daten.
// So können unterschiedliche Übertragungsprotokolle (HTTP und MQTT) transparent
// aus der Worker-Schleife heraus verwendet werden.
type Forwarder interface {
	// Send übermittelt eine Nachricht an den Upstream-Dienst.
	// Neben dem Topic und dem Payload wird auch der historische Zeitstempel übergeben,
	// um im Offline-Fall (Store & Forward) korrekte historische Messwerte anliefern zu können.
	Send(topic string, payload []byte, timestamp time.Time) error
	
	// IsConnected prüft, ob die Verbindung zum Upstream aktuell aktiv ist.
	IsConnected() bool
	
	// Connect stellt die initiale Verbindung zum Upstream-Dienst her oder re-initialisiert den Client.
	Connect() error
	
	// Close beendet alle aktiven Verbindungen sauber und gibt Ressourcen frei.
	Close()

	// Subscribe registriert einen Handler, der alle eingehenden Downstream-Nachrichten
	// vom Upstream-Broker empfängt (z.B. für Aktor-Steuerung: Cloud → lokaler Client).
	// Ein DownstreamHandler wird mit (topic, payload) aufgerufen.
	Subscribe(topics []string, handler DownstreamHandler) error

	// SetDownstreamHandler setzt den Callback fur Downstream-Nachrichten.
	// Kann NACH Mochi-Broker-Setup aufgerufen werden.
	SetDownstreamHandler(handler DownstreamHandler)
}

// DownstreamHandler ist die Signatur für den Empfang von Nachrichten vom Upstream-Broker.
type DownstreamHandler func(topic string, payload []byte)

// PermanentError signalisiert, dass eine Nachricht aufgrund ihrer Beschaffenheit
// vom Upstream-Dienst dauerhaft abgelehnt wird (z.B. ungültiges Topic)
// und nicht erneut gesendet werden sollte (Vermeidung einer Poison-Message-Endlosschleife).
type PermanentError struct {
	Err error
}

func (e *PermanentError) Error() string {
	return e.Err.Error()
}
