// Package broker initialisiert und verwaltet den lokalen, eingebetteten Mochi-MQTT-Broker
// sowie dessen StoreHook zur persistenten Zwischenspeicherung eingehender Nachrichten.
package broker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"strings"
	"sync"
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

// lastMsg speichert den Zeitpunkt und den Payload der zuletzt gesehenen Nachricht für ein Topic
type lastMsg struct {
	payload   []byte
	timestamp time.Time
}

// StoreHook implementiert einen Mochi-MQTT Hook (HookBase), welcher alle
// eingehenden Publish-Pakete abfängt und sie serialisiert in der BadgerDB ablegt.
type StoreHook struct {
	mqtt.HookBase
	// store ist die Referenz auf den lokalen BadgerDB-Wrapper
	store *storage.Store
	// filter enthält die optionalen Präfix-Regeln für das Speichern und Weiterleiten
	filter *config.FilterConf
	// dedupInterval gibt an, wie viele Millisekunden identische Payloads ignoriert werden
	dedupInterval int
	// dedupIgnoreKeys definiert JSON-Keys, die beim intelligenten Vergleich ignoriert werden
	dedupIgnoreKeys []string
	
	mu       sync.Mutex
	lastSeen map[string]lastMsg
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

	// 3. Filter: Deduplizierung (Debouncing) für schnelle Spam-Bursts
	if h.dedupInterval > 0 {
		h.mu.Lock()
		last, exists := h.lastSeen[topic]
		now := time.Now()
		
		if exists && time.Since(last.timestamp).Milliseconds() < int64(h.dedupInterval) {
			if isPayloadEffectivelyEqual(last.payload, pk.Payload, h.dedupIgnoreKeys) {
				h.mu.Unlock()
				// Zähle die weggeworfene Nachricht
				metrics.IncDeduplicated()
				// Ignoriere identische Nachricht im selben Zeitfenster
				return pk, nil
			}
		}
		
		// Map updaten
		h.lastSeen[topic] = lastMsg{
			payload:   append([]byte(nil), pk.Payload...), // Kopie erstellen, da pk.Payload überschrieben werden könnte
			timestamp: now,
		}
		h.mu.Unlock()
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

// isPayloadEffectivelyEqual vergleicht zwei Payloads. Zuerst via einfachem Byte-Vergleich.
// Wenn dedupIgnoreKeys gesetzt sind und beide Payloads gültiges JSON sind, werden die ignorierten
// Keys aus dem Vergleich herausgenommen.
func isPayloadEffectivelyEqual(a, b []byte, ignoreKeys []string) bool {
	if bytes.Equal(a, b) {
		return true
	}

	if len(ignoreKeys) == 0 {
		return false
	}

	// Falls beides potentielle JSON-Objekte sind, Smart-Vergleich machen
	if len(a) > 0 && a[0] == '{' && len(b) > 0 && b[0] == '{' {
		var mapA, mapB map[string]interface{}
		if err := json.Unmarshal(a, &mapA); err == nil {
			if err := json.Unmarshal(b, &mapB); err == nil {
				// Flüchtige Keys entfernen
				for _, k := range ignoreKeys {
					delete(mapA, k)
					delete(mapB, k)
				}
				// DeepEqual führt einen korrekten und typsicheren rekursiven Vergleich durch
				return reflect.DeepEqual(mapA, mapB)
			}
		}
	}

	return false
}

// StartLocalBroker initialisiert und startet den eingebetteten Mochi MQTT Server.
// Er stellt zwei Schnittstellen bereit:
// 1. TCP Listener: Auf diesem Port (standardmäßig 1883) lauschen wir auf lokale Geräte wie Zigbee2MQTT.
// 2. WebSocket Listener: Auf diesem Port (standardmäßig 1885) verbinden sich Web-Clients wie das Svelte Dashboard.
// Der Registrierte StoreHook stellt sicher, dass alle Publish-Nachrichten persistent gespeichert werden.
func StartLocalBroker(port int, wsPort int, store *storage.Store, filter *config.FilterConf, dedupInterval int, dedupIgnoreKeys []string) (*mqtt.Server, error) {
	server := mqtt.New(nil)

	// Anonyme Verbindungen erlauben (wichtig für lokale, einfache IoT-Geräte)
	_ = server.AddHook(new(auth.AllowHook), nil)

	// Hook für die BadgerDB-Pufferung registrieren
	_ = server.AddHook(&StoreHook{
		store:           store,
		filter:          filter,
		dedupInterval:   dedupInterval,
		dedupIgnoreKeys: dedupIgnoreKeys,
		lastSeen:        make(map[string]lastMsg),
	}, nil)

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

