package worker

import (
	"encoding/json"
	"log"
	"strconv"
	"time"

	"github.com/dgraph-io/badger/v4"
	"mlc2afmqttproxy/pkg/broker"
	"mlc2afmqttproxy/pkg/forwarder"
	"mlc2afmqttproxy/pkg/storage"
)

// Worker liest kontinuierlich aus der BadgerDB und sendet via Forwarder.
type Worker struct {
	store *storage.Store
	fwd   forwarder.Forwarder
	stop  chan struct{}
}

func New(s *storage.Store, f forwarder.Forwarder) *Worker {
	return &Worker{
		store: s,
		fwd:   f,
		stop:  make(chan struct{}),
	}
}

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

func (w *Worker) Stop() {
	close(w.stop)
	if w.fwd != nil {
		w.fwd.Close()
	}
}

func (w *Worker) processNext() {
	// 1. Prüfen ob Upstream verbunden ist
	if !w.fwd.IsConnected() {
		err := w.fwd.Connect()
		if err != nil {
			// Falls Offline, warte auf den nächsten Tick
			return
		}
	}

	// 2. Ältesten Eintrag aus der DB holen
	key, val, err := w.store.PeekFirst()
	if err != nil {
		if err != badger.ErrKeyNotFound {
			log.Printf("Fehler beim Lesen aus BadgerDB: %v", err)
		}
		return
	}

	// 3. Payload entpacken
	var wrapper broker.PayloadWrapper
	if err := json.Unmarshal(val, &wrapper); err != nil {
		log.Printf("Fehler beim Entpacken der BadgerDB-Nachricht: %v. Lösche korrupten Eintrag.", err)
		w.store.Delete(key)
		return
	}

	// Parse Timestamp aus dem Key
	var ts time.Time
	tsInt, err := strconv.ParseInt(string(key), 10, 64)
	if err == nil {
		ts = time.Unix(0, tsInt)
	} else {
		ts = time.Now()
	}

	// 4. Versuchen zu senden
	err = w.fwd.Send(wrapper.Topic, wrapper.Payload, ts)
	if err != nil {
		// Senden fehlgeschlagen, Eintrag bleibt in der DB für Retries
		return
	}

	// 5. Bei Erfolg Eintrag löschen
	err = w.store.Delete(key)
	if err != nil {
		log.Printf("Fehler beim Löschen des gepufferten Eintrags: %v", err)
	}
}
