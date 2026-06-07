// Package worker implementiert eine periodische Hintergrund-Goroutine (Worker),
// welche sequentiell die ältesten Nachrichten aus der BadgerDB ausliest,
// sie an den Upstream-Forwarder sendet und bei Erfolg aus der DB löscht.
package worker

import (
	"encoding/json"
	"log"
	"time"

	"github.com/dgraph-io/badger/v4"
	"mlc2afmqttproxy/pkg/broker"
	"mlc2afmqttproxy/pkg/forwarder"
	"mlc2afmqttproxy/pkg/metrics"
	"mlc2afmqttproxy/pkg/storage"
)

// Worker steuert den asynchronen Prozess des Sendens gepufferter Daten.
type Worker struct {
	// store ist die BadgerDB-Schnittstelle
	store *storage.Store
	// fwd ist der aktive HTTP- oder MQTT-Upstream-Forwarder
	fwd   forwarder.Forwarder
	// stop signalisiert dem Worker das Einstellen der Arbeit
	stop  chan struct{}
}

// New erzeugt eine neue Worker-Instanz, die auf der DB und dem Forwarder operiert.
func New(s *storage.Store, f forwarder.Forwarder) *Worker {
	return &Worker{
		store: s,
		fwd:   f,
		stop:  make(chan struct{}),
	}
}

// Start startet die periodische Worker-Schleife (alle 100ms) in einer eigenen Goroutine.
func (w *Worker) Start() {
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-w.stop:
				return
			case <-ticker.C:
				w.processNext()
			}
		}
	}()
}

// Stop beendet die Worker-Goroutine und schließt den Upstream-Forwarder.
func (w *Worker) Stop() {
	close(w.stop)
	if w.fwd != nil {
		w.fwd.Close()
	}
}

// processNext führt einen einzelnen Verarbeitungs- und Weiterleitungsschritt aus:
// 1. Verbindung zum Upstream sicherstellen.
// 2. Ältesten Puffer-Eintrag aus BadgerDB lesen.
// 3. Zeitstempel aus dem Datenbankschlüssel (RFC3339Nano) dekodieren.
// 4. Nachricht über Forwarder senden.
// 5. Bei erfolgreichem Versand: Eintrag aus der BadgerDB löschen.
func (w *Worker) processNext() {
	// 1. Prüfen, ob der Upstream-Client verbunden ist. Wenn nicht, verbinden.
	if !w.fwd.IsConnected() {
		err := w.fwd.Connect()
		if err != nil {
			// Falls der Upstream offline ist, warten wir still auf den nächsten Tick.
			return
		}
	}

	// 2. Den ältesten Eintrag (FIFO) aus der BadgerDB holen
	key, val, err := w.store.PeekFirst()
	if err != nil {
		if err != badger.ErrKeyNotFound {
			log.Printf("Fehler beim Lesen aus BadgerDB: %v", err)
		}
		return
	}

	// 3. Payload aus BadgerDB deserialisieren
	var wrapper broker.PayloadWrapper
	if err := json.Unmarshal(val, &wrapper); err != nil {
		log.Printf("Fehler beim Entpacken der BadgerDB-Nachricht: %v. Lösche korrupten Eintrag.", err)
		w.store.Delete(key)
		return
	}

	// Zeitstempel aus dem Key rekonstruieren.
	// Die Schlüssel werden in mochi.go als time.RFC3339Nano formatiert abgespeichert.
	var ts time.Time
	ts, err = time.Parse(time.RFC3339Nano, string(key))
	if err != nil {
		// Fallback auf time.Now(), falls das Schlüsselformat fehlerhaft sein sollte
		ts = time.Now()
	}

	// 4. Nachricht an Upstream senden.
	err = w.fwd.Send(wrapper.Topic, wrapper.Payload, ts)
	if err != nil {
		metrics.IncForwardFailed()
		// Senden fehlgeschlagen (z.B. Verbindungsunterbrechung während des Sendens).
		// Der Eintrag bleibt für einen erneuten Versuch (Retry) in der Datenbank liegen.
		return
	}

	metrics.IncForwarded()

	// 5. Nach erfolgreichem Versand den Eintrag aus der BadgerDB löschen
	err = w.store.Delete(key)
	if err != nil {
		log.Printf("Fehler beim Löschen des gepufferten Eintrags: %v", err)
	}
}

