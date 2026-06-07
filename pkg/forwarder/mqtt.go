package forwarder

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
	"mlc2afmqttproxy/pkg/config"
)

// MQTTForwarder implementiert die Forwarder-Schnittstelle unter Verwendung von MQTT 5 (via Eclipse autopaho).
// Er leitet Nachrichten an einen Upstream-Master/Cloud-Broker weiter und kann optional
// Zeitstempel in das JSON injizieren oder als MQTT 5 User Properties mitschicken.
type MQTTForwarder struct {
	// Upstream ist die Broker-URL (z.B. tcp://cloud.example.com:1883)
	Upstream       string
	// TimestampMode definiert, wie mit dem Zeitstempel verfahren wird ("none", "json_inject", "v5_property")
	TimestampMode  string
	// TimestampField ist der Name des Schlüssels bei "json_inject" oder "v5_property"
	TimestampField string
	// Rewrite enthält Umschreibregeln für das Topic
	Rewrite        *config.TopicRewriteConf
	
	// connManager verwaltet die automatische Verbindung (Reconnects etc.)
	connManager    *autopaho.ConnectionManager
	
	// username und password für Connect()
	username string
	password string
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

// Close trennt die Verbindung zum Upstream-Broker.
func (f *MQTTForwarder) Close() {
	if f.connManager != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		f.connManager.Disconnect(ctx)
	}
}
