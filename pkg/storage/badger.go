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
	
	db, err := badger.Open(opts)
	if err != nil {
		return nil, err
	}

	stopGC := make(chan struct{})

	// Garbage Collector Goroutine
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
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

