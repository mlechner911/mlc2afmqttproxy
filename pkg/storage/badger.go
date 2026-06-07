// Package storage stellt den BadgerDB-Wrapper bereit, welcher zur persistenten
// lokalen Pufferung (Store & Forward) verwendet wird.
package storage

import (
	"encoding/json"
	"log"
	"time"

	"github.com/dgraph-io/badger/v4"
)

// Store kapselt den Zugriff auf die lokale BadgerDB.
type Store struct {
	// db ist die zugrundeliegende BadgerDB-Instanz
	db *badger.DB
	// stopGC signalisiert der GC-Goroutine das Ende
	stopGC chan struct{}
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
			again:
				// Versucht das Value Log aufzuräumen (Schwelle bei 70% ungenutztem Speicher)
				err := db.RunValueLogGC(0.7)
				if err == nil {
					goto again
				}
			}
		}
	}()

	log.Printf("BadgerDB initialisiert in %s", path)
	return &Store{db: db, stopGC: stopGC}, nil
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
	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Set(key, val)
	})
}

// PeekFirst liest den ältesten Eintrag (den ersten Schlüssel in lexikographischer Reihenfolge)
// aus der Datenbank aus (FIFO-Prinzip).
// Gibt den Schlüssel, den Wert und ggf. badger.ErrKeyNotFound zurück.
func (s *Store) PeekFirst() (key []byte, val []byte, err error) {
	err = s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true
		opts.PrefetchSize = 1
		it := txn.NewIterator(opts)
		defer it.Close()

		it.Rewind()
		if it.Valid() {
			item := it.Item()
			key = item.KeyCopy(nil)
			val, err = item.ValueCopy(nil)
			return err
		}
		return badger.ErrKeyNotFound
	})
	return
}

// Delete entfernt einen Eintrag anhand seines Schlüssels.
func (s *Store) Delete(key []byte) error {
	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Delete(key)
	})
}

// GetSize liefert die ungefähre Anzahl der aktuell gepufferten Nachrichten.
// Zählt alle Einträge per Iterator (ohne Payload-Prefetch für hohe Effizienz).
func (s *Store) GetSize() (int, error) {
	var count int
	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			count++
		}
		return nil
	})
	return count, err
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
		for it.Rewind(); it.Valid(); it.Next() {
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
		for it.Rewind(); it.Valid(); it.Next() {
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

