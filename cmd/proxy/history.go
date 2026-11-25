package main

import (
	"context"
	"log"
	"path/filepath"

	"mlc2afmqttproxy/pkg/broker"
	"mlc2afmqttproxy/pkg/config"
	"mlc2afmqttproxy/pkg/devices"
	"mlc2afmqttproxy/pkg/forwarder"
	"mlc2afmqttproxy/pkg/history"
	"mlc2afmqttproxy/pkg/metrics"
	"mlc2afmqttproxy/pkg/storage"
	"mlc2afmqttproxy/pkg/worker"
)

// openHistory öffnet die Verlaufsstatistik unter <storage.path>/history. Lässt sie sich nicht
// öffnen, läuft der Proxy ohne sie weiter (nil) — die Weiterleitung hängt nicht daran.
func openHistory(cfg *config.Config) *history.Store {
	h, err := history.Open(filepath.Join(cfg.Storage.Path, "history"))
	if err != nil {
		log.Printf("[history] Verlauf nicht verfügbar: %v", err)
		return nil
	}
	return h
}

// openDevices legt die Gerätestatistik an, lädt den gespeicherten Stand und hängt sie an den
// lokalen Broker; sie wird mit dem Verlauf jede Minute gesichert.
func openDevices(cfg *config.Config, h *history.Store) *devices.Tracker {
	t := devices.New(cfg.MQTT.ZigbeeBaseTopic)
	path := filepath.Join(cfg.Storage.Path, "history", "devices.json")
	if err := t.Load(path); err != nil {
		log.Printf("[devices] %v", err)
	}
	broker.SetObserver(t.Observe)
	if h != nil {
		h.OnFlush = func() {
			if err := t.Save(path); err != nil {
				log.Printf("[devices] Sichern fehlgeschlagen: %v", err)
			}
		}
	}
	return t
}

// upstreamUp meldet, ob der Haupt-Upstream erreichbar ist: MQTT über den echten
// Verbindungszustand (Online); bei HTTP, das verbindungslos arbeitet, daran, ob der letzte
// Sendeversuch des Workers gelang.
func upstreamUp(fwd forwarder.Forwarder, w *worker.Worker) bool {
	if lr, ok := fwd.(forwarder.LinkReporter); ok && !lr.Online() {
		return false
	}
	return fwd.IsConnected() && !w.Failing()
}

// forwardState bestimmt den Zustand der Weiterleitung (Haupt-Upstream), nach Vorrang.
func forwardState(fwd forwarder.Forwarder, w *worker.Worker, paused func() bool) string {
	switch {
	case metrics.BufferFull():
		return history.StateBufferFull
	case !upstreamUp(fwd, w):
		return history.StateUpstreamDown
	case paused != nil && paused():
		return history.StatePaused
	default:
		return history.StateOK
	}
}

// sampler liefert die Messung für den Verlauf: Zähler seit dem Start, Pufferstand, Zustand.
func sampler(db *storage.Store, fwd forwarder.Forwarder, w *worker.Worker, dev *devices.Tracker, paused *func() bool) func() history.Sample {
	return func() history.Sample {
		st := metrics.GetStats()
		n, _ := db.GetSize()
		return history.Sample{
			Totals: history.Counters{
				Received:     int64(st.MessagesReceived),
				Filtered:     int64(st.MessagesFiltered),
				Deduplicated: int64(st.MessagesDeduplicated),
				Stored:       int64(st.MessagesStored),
				Forwarded:    int64(st.MessagesForwarded),
				Failed:       int64(st.MessagesForwardFailed),
				DeadLettered: int64(st.MessagesDeadLettered),
				Dropped:      int64(st.MessagesDroppedFull),
			},
			Buffer: int64(n),
			State:   forwardState(fwd, w, *paused),
			Devices: dev.Drain(),
		}
	}
}

// startHistory startet die Messungen; die zurückgegebene Funktion beendet sie und wartet, bis
// der letzte Stand geschrieben ist.
func startHistory(h *history.Store, sample func() history.Sample) (stop func()) {
	if h == nil {
		return func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.Run(ctx, sample)
	}()
	return func() {
		cancel()
		<-done
	}
}
