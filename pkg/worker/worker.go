// Package worker implementiert eine periodische Hintergrund-Goroutine (Worker),
// welche sequentiell die ältesten Nachrichten aus der BadgerDB ausliest,
// sie an den Upstream-Forwarder sendet und bei Erfolg aus der DB löscht.
package worker

import (
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/dgraph-io/badger/v4"
	"mlc2afmqttproxy/pkg/broker"
	"mlc2afmqttproxy/pkg/config"
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
	// cfg enthält die Worker-Konfigurationseinstellungen
	cfg   config.WorkerConf

	// Fehlerverfolgung für Exponential Backoff
	consecutiveFailures int
	nextAttempt         time.Time

	// pauseCheck (optional) lässt das Upstream-Senden ruhen, solange es true liefert
	// (Fernsteuerung „pause"/„drain"). Eingehende Nachrichten werden weiter im
	// Store&Forward-Puffer gehalten — kein Datenverlust; „resume" leert den Puffer.
	pauseCheck func() bool
}

// SetPauseCheck hinterlegt eine Funktion, die das Pausieren des Upstream-Versands
// steuert (true = ruhen). nil = nie pausieren (Standardverhalten).
func (w *Worker) SetPauseCheck(fn func() bool) { w.pauseCheck = fn }

// New erzeugt eine neue Worker-Instanz, die auf der DB, dem Forwarder und der Konfiguration operiert.
func New(s *storage.Store, f forwarder.Forwarder, cfg config.WorkerConf) *Worker {
	return &Worker{
		store: s,
		fwd:   f,
		stop:  make(chan struct{}),
		cfg:   cfg,
	}
}

// Start startet die periodische Worker-Schleife (Intervall über Config definiert) in einer eigenen Goroutine.
func (w *Worker) Start() {
	go func() {
		ticker := time.NewTicker(time.Duration(w.cfg.IntervalMs) * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-w.stop:
				return
			case <-ticker.C:
				w.runBatch()
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

// runBatch verarbeitet einen Batch von gepufferten Nachrichten in einer Schleife
// bis zu MaxBatchSize, um die DB effizient und schnell abzuarbeiten.
func (w *Worker) runBatch() {
	// Fernsteuerung „pause"/„drain": Versand ruhen lassen (Puffer bleibt erhalten).
	if w.pauseCheck != nil && w.pauseCheck() {
		return
	}
	for i := 0; i < w.cfg.MaxBatchSize; i++ {
		// Stop-Signal prüfen
		select {
		case <-w.stop:
			return
		default:
		}

		processed, err := w.processNext()
		if err != nil {
			// Sendevorgang oder Verbindung fehlgeschlagen (Backoff ist aktiv), Batch abbrechen.
			break
		}
		if !processed {
			// Keine weiteren Nachrichten in BadgerDB vorhanden.
			break
		}

		// Optionale künstliche Verzögerung zwischen Nachrichten im Batch zur Upstream-Drosselung
		if w.cfg.BatchDelayMs > 0 && i < w.cfg.MaxBatchSize-1 {
			select {
			case <-w.stop:
				return
			case <-time.After(time.Duration(w.cfg.BatchDelayMs) * time.Millisecond):
			}
		}
	}
}

// processNext führt einen einzelnen Verarbeitungs- und Weiterleitungsschritt aus:
// 1. Prüft, ob ein Backoff aktiv ist.
// 2. Verbindung zum Upstream sicherstellen.
// 3. Ältesten Puffer-Eintrag aus BadgerDB lesen.
// 4. Zeitstempel aus dem Datenbankschlüssel (RFC3339Nano) dekodieren.
// 5. Nachricht über Forwarder senden.
// 6. Bei erfolgreichem Versand: Eintrag aus der BadgerDB löschen.
// Gibt zurück, ob eine Nachricht erfolgreich verarbeitet wurde (processed) und ggf. einen Fehler.
func (w *Worker) processNext() (bool, error) {
	// 1. Backoff-Zeit prüfen
	if time.Now().Before(w.nextAttempt) {
		return false, nil
	}

	// 2. Prüfen, ob der Upstream-Client verbunden ist. Wenn nicht, verbinden.
	if !w.fwd.IsConnected() {
		err := w.fwd.Connect()
		if err != nil {
			w.handleFailure()
			return false, err
		}
	}

	// 3. Den ältesten Eintrag (FIFO) aus der BadgerDB holen
	key, val, err := w.store.PeekFirst()
	if err != nil {
		if err != badger.ErrKeyNotFound {
			log.Printf("Fehler beim Lesen aus BadgerDB: %v", err)
			return false, err
		}
		return false, nil
	}

	// 4. Payload aus BadgerDB deserialisieren
	var wrapper broker.PayloadWrapper
	if err := json.Unmarshal(val, &wrapper); err != nil {
		log.Printf("Fehler beim Entpacken der BadgerDB-Nachricht: %v. Lösche korrupten Eintrag.", err)
		w.store.Delete(key)
		return true, nil // true zurückgeben, da dieser Tabelleneintrag erledigt (gelöscht) ist
	}

	// Zeitstempel aus dem Key rekonstruieren
	var ts time.Time
	ts, err = time.Parse(time.RFC3339Nano, string(key))
	if err != nil {
		ts = time.Now()
	}

	// 5. Nachricht an Upstream senden
	err = w.fwd.Send(wrapper.Topic, wrapper.Payload, ts)
	if err != nil {
		log.Printf("[Worker] Fehler beim Senden an Upstream: %v", err)
		
		var permErr *forwarder.PermanentError
		if errors.As(err, &permErr) {
			log.Printf("[Worker] Nachricht dauerhaft abgelehnt (Poison Message), verwerfe Eintrag für Topic: %s", wrapper.Topic)
			w.store.Delete(key)
			return true, nil // Gilt als "verarbeitet", Queue geht weiter
		}
		
		metrics.IncForwardFailed()
		w.handleFailure()
		return false, err
	}

	metrics.IncForwarded()
	w.handleSuccess()

	// 6. Nach erfolgreichem Versand den Eintrag aus der BadgerDB löschen
	err = w.store.Delete(key)
	if err != nil {
		log.Printf("Fehler beim Löschen des gepufferten Eintrags: %v", err)
	}

	return true, nil
}

// handleFailure berechnet den Exponential Backoff und sperrt den Worker temporär für weitere Versuche.
func (w *Worker) handleFailure() {
	w.consecutiveFailures++
	tempFailures := w.consecutiveFailures
	if tempFailures > 10 {
		tempFailures = 10 // Capping bei 2^10, um Overflow zu vermeiden
	}

	backoff := time.Duration(w.cfg.RetryMinS) * time.Second * time.Duration(1<<uint(tempFailures-1))
	maxBackoff := time.Duration(w.cfg.RetryMaxS) * time.Second
	if backoff > maxBackoff {
		backoff = maxBackoff
	}

	w.nextAttempt = time.Now().Add(backoff)
	log.Printf("[Worker] Upstream-Verbindung oder Sendevorgang fehlgeschlagen. Nächster Versuch in %v (Fehlversuche: %d)", backoff, w.consecutiveFailures)
}

// handleSuccess setzt den Fehlerzähler und die Backoff-Sperre zurück.
func (w *Worker) handleSuccess() {
	w.consecutiveFailures = 0
	w.nextAttempt = time.Time{}
}
