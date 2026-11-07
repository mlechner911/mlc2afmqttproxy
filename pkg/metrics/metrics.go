package metrics

import "sync/atomic"

// Stats fasst alle Laufzeit-Metriken zusammen.
type Stats struct {
	MessagesReceived      uint64 `json:"messages_received_total"`
	MessagesStored        uint64 `json:"messages_stored_total"`
	MessagesForwarded     uint64 `json:"messages_forwarded_total"`
	MessagesForwardFailed uint64 `json:"messages_forward_failed_total"`
	MessagesDeduplicated  uint64 `json:"messages_deduplicated_total"`
	// Endgültig abgelehnt und in der Dead-Letter-Datei abgelegt (nicht weitergeleitet).
	MessagesDeadLettered uint64 `json:"messages_dead_lettered_total"`
	// Bei vollem Puffer nicht angenommen (Tail-Drop) — diese Werte fehlen im Nachweis.
	MessagesDroppedFull uint64 `json:"messages_dropped_buffer_full_total"`
}

var (
	messagesReceived      uint64
	messagesStored        uint64
	messagesForwarded     uint64
	messagesForwardFailed uint64
	messagesDeduplicated  uint64
	messagesDeadLettered  uint64
	messagesDroppedFull   uint64
)

// IncDeadLettered zählt eine endgültig abgelehnte, in der Dead-Letter-Datei abgelegte Nachricht.
func IncDeadLettered() { atomic.AddUint64(&messagesDeadLettered, 1) }

// IncDroppedFull zählt eine bei vollem Puffer verworfene Nachricht (Tail-Drop).
func IncDroppedFull() { atomic.AddUint64(&messagesDroppedFull, 1) }

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

// IncDeduplicated erhöht den Zähler für aufgrund von Debouncing verworfene Pakete.
func IncDeduplicated() {
	atomic.AddUint64(&messagesDeduplicated, 1)
}

// GetStats liefert den aktuellen Stand aller Zähler.
func GetStats() Stats {
	return Stats{
		MessagesReceived:      atomic.LoadUint64(&messagesReceived),
		MessagesStored:        atomic.LoadUint64(&messagesStored),
		MessagesForwarded:     atomic.LoadUint64(&messagesForwarded),
		MessagesForwardFailed: atomic.LoadUint64(&messagesForwardFailed),
		MessagesDeduplicated:  atomic.LoadUint64(&messagesDeduplicated),
		MessagesDeadLettered:  atomic.LoadUint64(&messagesDeadLettered),
		MessagesDroppedFull:   atomic.LoadUint64(&messagesDroppedFull),
	}
}
