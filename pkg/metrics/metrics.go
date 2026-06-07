package metrics

import "sync/atomic"

// Stats fasst alle Laufzeit-Metriken zusammen.
type Stats struct {
	MessagesReceived      uint64 `json:"messages_received_total"`
	MessagesStored        uint64 `json:"messages_stored_total"`
	MessagesForwarded     uint64 `json:"messages_forwarded_total"`
	MessagesForwardFailed uint64 `json:"messages_forward_failed_total"`
}

var (
	messagesReceived      uint64
	messagesStored        uint64
	messagesForwarded     uint64
	messagesForwardFailed uint64
)

// IncReceived erhöht den Zähler für lokal empfangene Pakete.
func IncReceived() {
	atomic.AddUint64(&messagesReceived, 1)
}

// IncStored erhöht den Zähler für erfolgreich in BadgerDB gespeicherte Pakete.
func IncStored() {
	atomic.AddUint64(&messagesStored, 1)
}

// IncForwarded erhöht den Zähler für erfolgreich weitergeleitete Pakete.
func IncForwarded() {
	atomic.AddUint64(&messagesForwarded, 1)
}

// IncForwardFailed erhöht den Zähler für fehlgeschlagene Sendeversuche.
func IncForwardFailed() {
	atomic.AddUint64(&messagesForwardFailed, 1)
}

// GetStats liefert den aktuellen Stand aller Zähler.
func GetStats() Stats {
	return Stats{
		MessagesReceived:      atomic.LoadUint64(&messagesReceived),
		MessagesStored:        atomic.LoadUint64(&messagesStored),
		MessagesForwarded:     atomic.LoadUint64(&messagesForwarded),
		MessagesForwardFailed: atomic.LoadUint64(&messagesForwardFailed),
	}
}
