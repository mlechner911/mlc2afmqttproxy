// Package storage stellt den BadgerDB-Wrapper bereit, welcher zur persistenten
// lokalen Pufferung (Store & Forward) verwendet wird.
package storage

import (
	"bytes"
	"encoding/json"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dgraph-io/badger/v4"
)

// Store kapselt den Zugriff auf die lokale BadgerDB.
type Store struct {
	// db ist die zugrundeliegende BadgerDB-Instanz
	db *badger.DB
	// stopGC signalisiert der GC-Goroutine das Ende
	stopGC chan struct{}

	// seekFrom ist ein Low-Watermark (kleinster zu betrachtender Key): Store & Forward
	// löscht jeden Eintrag nach dem Weiterleiten → es bleibt ein „Friedhof" aus
	// Lösch-Tombstones (FIFO-Timestamp-Keys, älteste zuerst). Ein Iterator.Rewind()
	// müsste den jedes Mal komplett durchscannen (das war der 100%-CPU-Bug). Da wir
	// streng FIFO abarbeiten und Keys monoton wachsen, starten wir die Iteration ab
	// dem zuletzt verarbeiteten/gelöschten Key und überspringen so die Tombstones.
	seekMu   sync.Mutex
	seekFrom []byte

	// count ist die In-Memory-Anzahl gepufferter (lebender) Einträge. Damit der
	// Leer-Check (PeekFirst) und das Dashboard (GetSize) NICHT iterieren müssen —
	// sonst scannen sie bei leerer Queue jedes Mal den Tombstone-Friedhof (100% CPU).
	count atomic.Int64
}

// seekStart positioniert den Iterator ab dem Low-Watermark (überspringt den
// Tombstone-Friedhof), beim Kaltstart von vorn.
func (s *Store) seekStart(it *badger.Iterator) {
	s.seekMu.Lock()
	from := s.seekFrom
	s.seekMu.Unlock()
	if from != nil {
		it.Seek(from)
	} else {
		it.Rewind()
	}
}

// advanceWatermark schiebt den Low-Watermark vorwärts (nie zurück).
func (s *Store) advanceWatermark(key []byte) {
	s.seekMu.Lock()
	if s.seekFrom == nil || bytes.Compare(key, s.seekFrom) > 0 {
		s.seekFrom = append([]byte(nil), key...)
	}
	s.seekMu.Unlock()
}

// InitBadger initialisiert eine persistente BadgerDB am angegebenen Pfad.
// Startet zusätzlich eine Hintergrund-Goroutine, die periodisch (alle 5 Minuten)
// eine Garbage Collection (GC) auf dem Value Log durchführt, um Speicherplatz freizugeben.
func InitBadger(path string) (*Store, error) {
	opts := badger.DefaultOptions(path)
	// Internen Logger deaktivieren, um Log-Rauschen zu reduzieren
	opts.Logger = nil
	// Kleine Value-Log-Dateien (32 MB statt Default 1 GB): BadgerDB gibt gelöschten
	// (= weitergeleiteten) Speicher erst frei, wenn eine vlog-Datei voll UND zu ≥70 %
	// Müll ist. Mit 1-GB-Dateien wächst der Value-Log unter Last bis ~1 GB, bevor GC
	// greift — das ließ die (an db.Size() gemessene) „Größe" fälschlich das Tail-Drop-
	// Limit reißen, obwohl real fast nichts gepuffert war. Kleine Dateien rotieren
	// schnell → GC kann zeitnah freigeben.
	opts.ValueLogFileSize = 32 << 20

	db, err := badger.Open(opts)
	if err != nil {
		return nil, err
	}

	stopGC := make(chan struct{})

	// Garbage Collector Goroutine (häufig, damit weitergeleitete Einträge zügig aus
	// dem Value-Log verschwinden und die Größe der Realität folgt).
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopGC:
				return
			case <-ticker.C:
				// Reklamierbare Value-Log-Dateien aufräumen (Schwelle 70% verwerfbar),
				// aber GEDECKELT: RunValueLogGC liefert bei Erfolg nil — ohne Deckel
				// kann das bei kleinen 32-MB-Dateien + Store-&-Forward-Churn (alles
				// wird nach dem Forwarden gelöscht → fast alles verwerfbar) endlos nil
				// zurückgeben und 100% CPU brennen. 8 Rewrites/Min (≈256 MB) genügen
				// weit; sonst bis zum nächsten Tick warten.
				for n := 0; n < 8; n++ {
					if err := db.RunValueLogGC(0.7); err != nil {
						break // ErrNoRewrite o.ä. → nichts (mehr) aufzuräumen
					}
				}
			}
		}
	}()

	store := &Store{db: db, stopGC: stopGC}
	// Einmaliger Startup-Scan, um den Puffer-Zähler zu initialisieren (danach rein
	// in-memory via Push/Delete gepflegt).
	if n, err := store.scanCount(); err == nil {
		store.count.Store(int64(n))
	}
	log.Printf("BadgerDB initialisiert in %s (%d gepuffert)", path, store.count.Load())
	return store, nil
}

// scanCount zählt die aktuell gepufferten (lebenden) Einträge per Voll-Iteration.
// Nur beim Start (einmalig) — im Betrieb dient der In-Memory-Zähler.
func (s *Store) scanCount() (int, error) {
	var c int
	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			c++
		}
		return nil
	})
	return c, err
}

// Close schließt die BadgerDB-Instanz sauber ab.
func (s *Store) Close() {
	if s.stopGC != nil {
		close(s.stopGC)
	}
	if s.db != nil {
		s.db.Close()
	}
}

// Push speichert einen Schlüssel-Wert-Eintrag in der Datenbank.
func (s *Store) Push(key []byte, val []byte) error {
	err := s.db.Update(func(txn *badger.Txn) error {
		return txn.Set(key, val)
	})
	if err == nil {
		s.count.Add(1)
	}
	return err
}

// PeekFirst liest den ältesten Eintrag (den ersten Schlüssel in lexikographischer Reihenfolge)
// aus der Datenbank aus (FIFO-Prinzip).
// Gibt den Schlüssel, den Wert und ggf. badger.ErrKeyNotFound zurück.
func (s *Store) PeekFirst() (key []byte, val []byte, err error) {
	// Leer? Dann gar nicht iterieren (kein Tombstone-Scan).
	if s.count.Load() <= 0 {
		return nil, nil, badger.ErrKeyNotFound
	}
	err = s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true
		opts.PrefetchSize = 1
		it := txn.NewIterator(opts)
		defer it.Close()

		s.seekStart(it) // ab Low-Watermark statt Rewind → Tombstones überspringen
		if it.Valid() {
			item := it.Item()
			key = item.KeyCopy(nil)
			s.advanceWatermark(key)
			val, err = item.ValueCopy(nil)
			return err
		}
		// Zähler sagte „nicht leer", DB ist es aber → Drift korrigieren.
		s.count.Store(0)
		return badger.ErrKeyNotFound
	})
	return
}

// Delete entfernt einen Eintrag anhand seines Schlüssels.
func (s *Store) Delete(key []byte) error {
	err := s.db.Update(func(txn *badger.Txn) error {
		return txn.Delete(key)
	})
	if err == nil {
		s.count.Add(-1)
		s.advanceWatermark(key) // gelöschten Key nicht mehr anfassen
	}
	return err
}

// GetSize liefert die Anzahl der aktuell gepufferten Nachrichten — aus dem
// In-Memory-Zähler (O(1)), KEIN Iterator. Verhindert den Tombstone-Voll-Scan bei
// jedem Dashboard-Stats-Poll.
func (s *Store) GetSize() (int, error) {
	n := s.count.Load()
	if n < 0 {
		n = 0
	}
	return int(n), nil
}

// GetDiskSizeMB liefert die aktuelle Größe der BadgerDB auf der Festplatte in Megabyte
// (LSM + Value Log, inkl. noch nicht per GC freigegebenem Müll). NUR fürs Dashboard —
// NICHT fürs Tail-Drop-Limit: dieser Wert hinkt der Realität nach (Value-Log-Churn).
func (s *Store) GetDiskSizeMB() int64 {
	lsm, vlog := s.db.Size()
	return (lsm + vlog) / (1024 * 1024)
}

// GetPendingSizeMB liefert die tatsächlich GEPUFFERTE (noch nicht weitergeleitete)
// Datenmenge in Megabyte — die Summe aus Key- und Value-Größe aller LEBENDEN Einträge.
// Maß für das Tail-Drop-Limit: weitergeleitete (gelöschte) Nachrichten zählen NICHT
// mehr mit, unabhängig davon, ob die GC den Value-Log schon aufgeräumt hat.
// ValueSize() kommt aus den LSM-Metadaten → kein Laden der Werte nötig.
func (s *Store) GetPendingSizeMB() int64 {
	var total int64
	_ = s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()
		// Ab Low-Watermark (lebende Keys ≥ Watermark) statt Rewind → Tombstones
		// nicht durchscannen. Wird pro eingehender Nachricht fürs Tail-Drop-Limit
		// aufgerufen (heißer Pfad).
		for s.seekStart(it); it.Valid(); it.Next() {
			item := it.Item()
			total += int64(item.KeySize()) + item.ValueSize()
		}
		return nil
	})
	return total / (1024 * 1024)
}

// GetRecent liest die ältesten X Einträge aus der Datenbank aus,
// decodiert sie und hängt den Schlüssel als "_timestamp" an (für das Web-Dashboard).
func (s *Store) GetRecent(limit int) ([]map[string]any, error) {
	var result []map[string]any
	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = limit
		it := txn.NewIterator(opts)
		defer it.Close()

		count := 0
		for s.seekStart(it); it.Valid(); it.Next() { // ab Watermark, Tombstones überspringen
			if count >= limit {
				break
			}
			item := it.Item()
			err := item.Value(func(v []byte) error {
				var data map[string]any
				if err := json.Unmarshal(v, &data); err == nil {
					data["_timestamp"] = string(item.Key())
					result = append(result, data)
				}
				return nil
			})
			if err != nil {
				return err
			}
			count++
		}
		return nil
	})
	return result, err
}
