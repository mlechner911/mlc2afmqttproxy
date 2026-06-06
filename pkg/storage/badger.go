package storage

import (
	"encoding/json"
	"log"
	"time"

	"github.com/dgraph-io/badger/v4"
)

type Store struct {
	db *badger.DB
}

// InitBadger initialisiert die lokale BadgerDB.
func InitBadger(path string) (*Store, error) {
	opts := badger.DefaultOptions(path)
	opts.Logger = nil 
	
	db, err := badger.Open(opts)
	if err != nil {
		return nil, err
	}

	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
		again:
			err := db.RunValueLogGC(0.7)
			if err == nil {
				goto again
			}
		}
	}()

	log.Printf("BadgerDB initialisiert in %s", path)
	return &Store{db: db}, nil
}

func (s *Store) Close() {
	if s.db != nil {
		s.db.Close()
	}
}

// Push speichert einen Wert in der Datenbank
func (s *Store) Push(key []byte, val []byte) error {
	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Set(key, val)
	})
}

// PeekFirst liest den ältesten Eintrag aus der Datenbank (FIFO).
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

// GetSize liefert die ungefähre Anzahl der gepufferten Nachrichten.
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

// GetRecent liest die ältesten X Einträge aus (für die UI).
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
