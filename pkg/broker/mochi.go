// Package broker initialisiert und verwaltet den lokalen, eingebetteten Mochi-MQTT-Broker
// sowie dessen StoreHook zur persistenten Zwischenspeicherung eingehender Nachrichten.
package broker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"mlc2afmqttproxy/pkg/config"
	"mlc2afmqttproxy/pkg/metrics"
	"mlc2afmqttproxy/pkg/storage"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/mochi-mqtt/server/v2/packets"
)

// PayloadWrapper umschließt das MQTT-Topic und die eigentlichen Nutzdaten (Payload),
// um sie gemeinsam als ein JSON-Dokument in der BadgerDB speichern zu können.
type PayloadWrapper struct {
	// Das ursprüngliche MQTT-Topic
	Topic   string `json:"topic"`
	// Die rohen Payload-Bytes der Nachricht
	Payload []byte `json:"payload"`
}

// StoreHook implementiert einen Mochi-MQTT Hook (HookBase), welcher alle
// eingehenden Publish-Pakete abfängt und sie serialisiert in der BadgerDB ablegt.
type StoreHook struct {
	mqtt.HookBase
	// store ist die Referenz auf den lokalen BadgerDB-Wrapper
	store *storage.Store
	// filter enthält die optionalen Präfix-Regeln für das Speichern und Weiterleiten
	filter *config.FilterConf
}

// ID liefert den eindeutigen Bezeichner des Hooks zurück.
func (h *StoreHook) ID() string {
	return "store-hook"
}

// Provides prüft, welche Event-Typen dieser Hook verarbeitet.
// In unserem Fall wird nur der OnPublish-Event abgefangen.
func (h *StoreHook) Provides(b byte) bool {
	return bytes.Contains([]byte{
		mqtt.OnPublish,
	}, []byte{b})
}

// OnPublish wird aufgerufen, wenn ein Client eine Nachricht auf dem Broker publiziert.
// Die Nachricht wird abgefangen, in einen PayloadWrapper gepackt und mit dem
// aktuellen Zeitstempel (für die FIFO-Abarbeitung) in der BadgerDB gespeichert.
func (h *StoreHook) OnPublish(cl *mqtt.Client, pk packets.Packet) (packets.Packet, error) {
	metrics.IncReceived()

	topic := pk.TopicName

	// 1. Filter: AllowedPrefixes (Whitelist)
	if h.filter != nil && len(h.filter.AllowedPrefixes) > 0 {
		allowed := false
		for _, prefix := range h.filter.AllowedPrefixes {
			if strings.HasPrefix(topic, prefix) {
				allowed = true
				break
			}
		}
		if !allowed {
			// Topic nicht in der Whitelist -> Verwerfen (nicht speichern)
			return pk, nil
		}
	}

	// 2. Filter: IgnoredPrefixes (Blacklist)
	if h.filter != nil && len(h.filter.IgnoredPrefixes) > 0 {
		for _, prefix := range h.filter.IgnoredPrefixes {
			if strings.HasPrefix(topic, prefix) {
				// Topic in der Blacklist -> Verwerfen (nicht speichern)
				return pk, nil
			}
		}
	}

	// Erstelle Schlüssel basierend auf UTC-Zeitstempel in RFC3339Nano (für korrekte lexikographische FIFO-Sortierung in Badger)
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

	// Speichern in BadgerDB
	if err := h.store.Push(key, val); err != nil {
		log.Printf("Fehler beim Speichern der MQTT-Nachricht in BadgerDB: %v", err)
	} else {
		metrics.IncStored()
	}

	return pk, nil
}

// StartLocalBroker startet den eingebetteten Mochi MQTT Broker.
// Er stellt zwei Schnittstellen bereit:
// 1. TCP Listener: Auf diesem Port (standardmäßig 1883) lauschen wir auf lokale Geräte wie Zigbee2MQTT.
// 2. WebSocket Listener: Auf diesem Port (standardmäßig 1885) verbinden sich Web-Clients wie das Svelte Dashboard.
// Der Registrierte StoreHook stellt sicher, dass alle Publish-Nachrichten persistent gespeichert werden.
func StartLocalBroker(port int, wsPort int, store *storage.Store, filter *config.FilterConf) (*mqtt.Server, error) {
	server := mqtt.New(nil)

	// Anonyme Verbindungen erlauben (wichtig für lokale, einfache IoT-Geräte)
	_ = server.AddHook(new(auth.AllowHook), nil)

	// Hook für die BadgerDB-Pufferung registrieren
	_ = server.AddHook(&StoreHook{store: store, filter: filter}, nil)

	// Lokaler TCP Listener einrichten
	address := fmt.Sprintf(":%d", port)
	err := server.AddListener(listeners.NewTCP(listeners.Config{
		ID:      "tcp-local",
		Address: address,
	}))
	if err != nil {
		return nil, err
	}

	// Lokaler WebSocket Listener für das Live Dashboard einrichten
	wsAddress := fmt.Sprintf(":%d", wsPort)
	err = server.AddListener(listeners.NewWebsocket(listeners.Config{
		ID:      "ws-local",
		Address: wsAddress,
	}))
	if err != nil {
		return nil, err
	}

	// Server asynchron starten
	go func() {
		log.Printf("Starte lokalen Mochi Broker auf %s", address)
		err := server.Serve()
		if err != nil {
			log.Fatalf("Mochi broker beendet mit Fehler: %v", err)
		}
	}()

	return server, nil
}

