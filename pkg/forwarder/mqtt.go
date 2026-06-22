package forwarder

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
	"mlc2afmqttproxy/pkg/config"
)

// MQTTForwarder implementiert die Forwarder-Schnittstelle unter Verwendung von MQTT 5 (via Eclipse autopaho).
// Er leitet Nachrichten an einen Upstream-Master/Cloud-Broker weiter und kann optional
// Zeitstempel in das JSON injizieren oder als MQTT 5 User Properties mitschicken.
// Unterstützt Downstream: Subscribe vom Upstream-Broker und Forward an lokale Clients.
type MQTTForwarder struct {
	// Upstream ist die Broker-URL (z.B. tcp://cloud.example.com:1883)
	Upstream string
	// TimestampMode definiert, wie mit dem Zeitstempel verfahren wird ("none", "json_inject", "v5_property")
	TimestampMode string
	// TimestampField ist der Name des Schlüssels bei "json_inject" oder "v5_property"
	TimestampField string
	// Rewrite enthält Umschreibregeln für das Topic
	Rewrite *config.TopicRewriteConf

	// connManager verwaltet die automatische Verbindung (Reconnects etc.)
	connManager *autopaho.ConnectionManager

	// username und password für Connect()
	username string
	password string

	// downHandler ist der Callback für eingehende Downstream-Nachrichten (Cloud → lokal)
	downHandler DownstreamHandler
	// downSubscribed verhindert doppelte Subscribe-Aufrufe bei Reconnects
	downSubscribed bool
	// downTopics speichert die konfigurierten Downstream-Topics
	downTopics []string
	// downRewrite enthält Umschreibregeln für Downstream-Topics (Cloud → lokal)
	downRewrite *config.TopicRewriteConf
	// downMu schützt downHandler/Subscription-Status
	downMu sync.Mutex
	// downHandlerRegistered verhindert doppeltes Registrieren des Publish-Handlers
	downHandlerRegistered bool
}

// NewMQTTForwarder erstellt und konfiguriert einen neuen MQTTForwarder für Upstream-MQTT.
func NewMQTTForwarder(upstream, username, password, timestampMode, timestampField string, rewrite *config.TopicRewriteConf) *MQTTForwarder {
	return &MQTTForwarder{
		Upstream:       upstream,
		username:       username,
		password:       password,
		TimestampMode:  timestampMode,
		TimestampField: timestampField,
		Rewrite:        rewrite,
	}
}

// Connect baut die initiale Verbindung zum Upstream-Broker auf.
func (f *MQTTForwarder) Connect() error {
	u, err := url.Parse(f.Upstream)
	if err != nil {
		return fmt.Errorf("ungültige Upstream-URL %s: %v", f.Upstream, err)
	}

	clientConfig := autopaho.ClientConfig{
		ServerUrls: []*url.URL{u},
		KeepAlive:  20,
		CleanStartOnInitialConnection: false,
		SessionExpiryInterval:         0xFFFFFFFF,
		OnConnectionUp: func(cm *autopaho.ConnectionManager, connAck *paho.Connack) {
			log.Printf("[MQTT-Upstream] Erfolgreich verbunden mit %s (MQTT 5)", f.Upstream)

			// Downstream Publish-Handler registrieren (einmalig)
			f.downMu.Lock()
			defer f.downMu.Unlock()
			if f.downHandler != nil && !f.downHandlerRegistered {
				cm.AddOnPublishReceived(f.handlePublishReceived)
				f.downHandlerRegistered = true
				log.Printf("[MQTT-Downstream] Publish-Handler registriert")
			}

			// Downstream Subscribe bei Connect/Reconnect
			if f.downTopics != nil && !f.downSubscribed {
				for _, topic := range f.downTopics {
					if sub, err := cm.Subscribe(context.Background(), &paho.Subscribe{
						Subscriptions: []paho.SubscribeOptions{
							{Topic: topic, QoS: 1},
						},
					}); err != nil {
						log.Printf("[MQTT-Downstream] Subscribe für '%s' fehlgeschlagen: %v", topic, err)
					} else {
						log.Printf("[MQTT-Downstream] subscribed: %v", sub)
					}
				}
				f.downSubscribed = true
			}
		},
		OnConnectError: func(err error) {
			log.Printf("[MQTT-Upstream] Verbindungsfehler: %v", err)
		},
		ClientConfig: paho.ClientConfig{
			ClientID: "mlc2af-proxy-forwarder",
		},
	}

	if f.username != "" {
		clientConfig.SetUsernamePassword(f.username, []byte(f.password))
	}

	cm, err := autopaho.NewConnection(context.Background(), clientConfig)
	if err != nil {
		return err
	}

	f.connManager = cm

	// Wait for the connection to be up
	err = cm.AwaitConnection(context.Background())
	if err != nil {
		return err
	}

	return nil
}

// IsConnected prüft den aktuellen Verbindungsstatus.
func (f *MQTTForwarder) IsConnected() bool {
	if f.connManager == nil {
		return false
	}
	return true
}

// Send führt optional Topic-Umschreibungen aus, verarbeitet den Zeitstempel
// und sendet die Nachricht mit QoS 1 an den Upstream-Broker.
// Setzt zusätzlich das User Property "origin=local" zur Loop-Detection.
func (f *MQTTForwarder) Send(topic string, payload []byte, timestamp time.Time) error {
	if f.connManager == nil {
		return fmt.Errorf("upstream mqtt client is not initialized")
	}

	// Vorabprüfung auf Wildcards (verboten beim Publizieren)
	if strings.ContainsAny(topic, "+#") {
		return &PermanentError{Err: fmt.Errorf("invalid topic contains wildcard: %s", topic)}
	}

	// 1. Topic-Umschreibung
	if f.Rewrite != nil && f.Rewrite.MatchPrefix != "" {
		if strings.HasPrefix(topic, f.Rewrite.MatchPrefix) {
			topic = f.Rewrite.ReplaceWith + strings.TrimPrefix(topic, f.Rewrite.MatchPrefix)
		}
	}

	finalPayload := payload
	var userProps []paho.UserProperty

	// 2. Zeitstempel-Behandlung
	switch f.TimestampMode {
	case "json_inject":
		var data map[string]any
		if err := json.Unmarshal(payload, &data); err == nil {
			if _, exists := data[f.TimestampField]; !exists {
				data[f.TimestampField] = timestamp.UnixMilli()
				if newPayload, err := json.Marshal(data); err == nil {
					finalPayload = newPayload
				}
			}
		}
	case "v5_property":
		// Bei v5_property packen wir den Zeitstempel in den MQTT 5 Header
		userProps = append(userProps, paho.UserProperty{
			Key:   f.TimestampField,
			Value: strconv.FormatInt(timestamp.UnixMilli(), 10),
		})
	}

	// origin="local" zur Loop-Detection
	userProps = append(userProps, paho.UserProperty{
		Key:   "origin",
		Value: "local",
	})

	// 3. Veröffentlichen mit QoS 1
	msg := &paho.Publish{
		Topic:   topic,
		QoS:     1,
		Payload: finalPayload,
	}

	if len(userProps) > 0 {
		msg.Properties = &paho.PublishProperties{
			User: userProps,
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := f.connManager.Publish(ctx, msg)
	if err != nil {
		return err
	}

	log.Printf("[MQTT-Upstream] Erfolgreich gesendet (MQTT5): Topic='%s', %d bytes", topic, len(finalPayload))
	return nil
}

// Subscribe registriert Topics und einen Handler für eingehende Nachrichten vom Upstream-Broker.
func (f *MQTTForwarder) Subscribe(topics []string, handler DownstreamHandler) error {
	if len(topics) == 0 {
		return nil
	}

	f.downMu.Lock()
	defer f.downMu.Unlock()

	f.downTopics = topics
	f.downHandler = handler
	// downSubscribed bleibt false → wird via OnConnectionUp beim Connect gesetzt
	// downHandlerRegistered bleibt false → wird via OnConnectionUp beim Connect gesetzt (einmalig)

	return nil
}

// SetDownRewrite setzt die Topic-Umschreibregel für Downstream-Nachrichten.
// SetDownstreamHandler setzt den Callback fur Downstream-Nachrichten.
func (f *MQTTForwarder) SetDownstreamHandler(h DownstreamHandler) {
	f.downMu.Lock()
	defer f.downMu.Unlock()
	f.downHandler = h
}
func (f *MQTTForwarder) SetDownRewrite(rewrite *config.TopicRewriteConf) {
	f.downMu.Lock()
	defer f.downMu.Unlock()
	f.downRewrite = rewrite
}

// handlePublishReceived verarbeitet eingehende Nachrichten vom Upstream-Broker.
func (f *MQTTForwarder) handlePublishReceived(pr autopaho.PublishReceived) (bool, error) {
	// Loop-Detection
	if pr.Packet.Properties != nil {
		for _, prop := range pr.Packet.Properties.User {
			if prop.Key == "origin" && prop.Value == "local" {
				return true, nil
			}
		}
	}

	topic := pr.Packet.Topic
	payload := pr.Packet.Payload

	// Topic-Umschreibung
	if f.downRewrite != nil && f.downRewrite.MatchPrefix != "" {
		if strings.HasPrefix(topic, f.downRewrite.MatchPrefix) {
			topic = f.downRewrite.ReplaceWith + strings.TrimPrefix(topic, f.downRewrite.MatchPrefix)
		}
	}

	log.Printf("[MQTT-Downstream] Empfangen: Topic='%s', %d bytes", topic, len(payload))

	if f.downHandler != nil {
		f.downHandler(topic, payload)
	}

	return true, nil
}

// Close trennt die Verbindung zum Upstream-Broker.
func (f *MQTTForwarder) Close() {
	if f.connManager != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		f.connManager.Disconnect(ctx)
	}
}
