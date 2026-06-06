package broker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"mlc2afmqttproxy/pkg/storage"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/mochi-mqtt/server/v2/packets"
)

// PayloadWrapper umschließt das Topic und die eigentlichen Daten
type PayloadWrapper struct {
	Topic   string `json:"topic"`
	Payload []byte `json:"payload"`
}

type StoreHook struct {
	mqtt.HookBase
	store *storage.Store
}

func (h *StoreHook) ID() string {
	return "store-hook"
}

func (h *StoreHook) Provides(b byte) bool {
	return bytes.Contains([]byte{
		mqtt.OnPublish,
	}, []byte{b})
}

func (h *StoreHook) OnPublish(cl *mqtt.Client, pk packets.Packet) (packets.Packet, error) {
	// Erstelle Schlüssel basierend auf Zeitstempel (für FIFO Sortierung in BadgerDB)
	key := []byte(time.Now().UTC().Format(time.RFC3339Nano))

	wrapper := PayloadWrapper{
		Topic:   pk.TopicName,
		Payload: pk.Payload,
	}

	val, err := json.Marshal(wrapper)
	if err != nil {
		log.Printf("Fehler beim Serialisieren der MQTT-Nachricht: %v", err)
		return pk, err
	}

	// Speichern in DB
	if err := h.store.Push(key, val); err != nil {
		log.Printf("Fehler beim Speichern der MQTT-Nachricht in BadgerDB: %v", err)
	}

	return pk, nil
}

// StartLocalBroker startet den eingebetteten Mochi MQTT Broker.
func StartLocalBroker(port int, store *storage.Store) (*mqtt.Server, error) {
	server := mqtt.New(nil)

	// Anonyme Verbindungen erlauben (nur für lokales Zigbee2MQTT)
	_ = server.AddHook(new(auth.AllowHook), nil)

	// Hook für BadgerDB Registrieren
	_ = server.AddHook(&StoreHook{store: store}, nil)

	// Lokaler TCP Listener, auf den Zigbee2MQTT pusht
	address := fmt.Sprintf(":%d", port)
	err := server.AddListener(listeners.NewTCP(listeners.Config{
		ID:      "tcp-local",
		Address: address,
	}))
	if err != nil {
		return nil, err
	}

	go func() {
		log.Printf("Starte lokalen Mochi Broker auf %s", address)
		err := server.Serve()
		if err != nil {
			log.Fatalf("Mochi broker beendet mit Fehler: %v", err)
		}
	}()

	return server, nil
}
