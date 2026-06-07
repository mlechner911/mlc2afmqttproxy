package forwarder

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"mlc2afmqttproxy/pkg/config"
)

// MQTTForwarder implementiert die Forwarder-Schnittstelle unter Verwendung von MQTT 3.1.1 (via Eclipse Paho).
// Er leitet Nachrichten an einen Upstream-Master/Cloud-Broker weiter und kann optional
// Zeitstempel in das JSON injizieren oder Topics umschreiben.
type MQTTForwarder struct {
	// Upstream ist die Broker-URL (z.B. tcp://cloud.example.com:1883)
	Upstream       string
	// TimestampMode definiert, wie mit dem Zeitstempel verfahren wird ("none", "json_inject")
	TimestampMode  string
	// TimestampField ist der Name des Schlüssels bei "json_inject"
	TimestampField string
	// Rewrite enthält Umschreibregeln für das Topic
	Rewrite        *config.TopicRewriteConf
	// client ist der Eclipse Paho MQTT v3 Client
	client         paho.Client
}

// NewMQTTForwarder erstellt und konfiguriert einen neuen MQTTForwarder für Upstream-MQTT.
// Es wird AutoReconnect aktiviert und Verbindungs-Events werden geloggt.
func NewMQTTForwarder(upstream, username, password, timestampMode, timestampField string, rewrite *config.TopicRewriteConf) *MQTTForwarder {
	opts := paho.NewClientOptions()
	opts.AddBroker(upstream)
	opts.SetClientID("mlc2af-proxy-forwarder") // Eindeutige Client-ID für den Upstream-Broker
	
	if username != "" {
		opts.SetUsername(username)
	}
	if password != "" {
		opts.SetPassword(password)
	}

	// Automatischer Reconnect wird von Paho im Hintergrund abgewickelt
	opts.SetAutoReconnect(true)
	opts.SetOnConnectHandler(func(c paho.Client) {
		log.Printf("[MQTT-Upstream] Erfolgreich verbunden mit %s", upstream)
	})
	opts.SetConnectionLostHandler(func(c paho.Client, err error) {
		log.Printf("[MQTT-Upstream] Verbindung verloren: %v", err)
	})

	return &MQTTForwarder{
		Upstream:       upstream,
		TimestampMode:  timestampMode,
		TimestampField: timestampField,
		Rewrite:        rewrite,
		client:         paho.NewClient(opts),
	}
}

// Connect baut die initiale Verbindung zum Upstream-Broker auf. Blockiert bis zur Verbindung oder bis zum Fehler.
func (f *MQTTForwarder) Connect() error {
	token := f.client.Connect()
	token.Wait()
	if token.Error() != nil {
		return token.Error()
	}
	return nil
}

// IsConnected prüft den aktuellen Verbindungsstatus des Paho Clients.
func (f *MQTTForwarder) IsConnected() bool {
	if f.client == nil {
		return false
	}
	return f.client.IsConnectionOpen() || f.client.IsConnected()
}

// Send führt optional Topic-Umschreibungen aus, injiziert bei Bedarf den historischen Zeitstempel
// in ein JSON-Payload (sofern konfiguriert und der Key fehlt) und sendet die Nachricht mit QoS 1 (At least once)
// an den Upstream-Broker.
func (f *MQTTForwarder) Send(topic string, payload []byte, timestamp time.Time) error {
	if !f.IsConnected() {
		return fmt.Errorf("upstream mqtt client is not connected")
	}

	// 1. Topic-Umschreibung (z.B. "zigbee2mqtt/sensor1" -> "cloud/sensor1")
	if f.Rewrite != nil && f.Rewrite.MatchPrefix != "" {
		if strings.HasPrefix(topic, f.Rewrite.MatchPrefix) {
			topic = f.Rewrite.ReplaceWith + strings.TrimPrefix(topic, f.Rewrite.MatchPrefix)
		}
	}

	finalPayload := payload

	// 2. Zeitstempel-Injektion in das JSON-Payload ("json_inject")
	if f.TimestampMode == "json_inject" {
		var data map[string]any
		// Versuche Payload als JSON zu parsen
		if err := json.Unmarshal(payload, &data); err == nil {
			// "Inject if absent": Überschreibe niemals einen existierenden Wert im JSON
			if _, exists := data[f.TimestampField]; !exists {
				data[f.TimestampField] = timestamp.UnixMilli()
				if newPayload, err := json.Marshal(data); err == nil {
					finalPayload = newPayload
				}
			}
		}
	}

	// 3. Veröffentlichen mit QoS 1 (Mindestens einmalige Zustellung)
	token := f.client.Publish(topic, 1, false, finalPayload)
	token.Wait()
	
	if token.Error() != nil {
		return token.Error()
	}

	log.Printf("[MQTT-Upstream] Erfolgreich gesendet: Topic='%s', %d bytes", topic, len(finalPayload))
	return nil
}

// Close trennt die Verbindung zum Upstream-Broker sauber (mit 250ms Quiesce-Zeit).
func (f *MQTTForwarder) Close() {
	if f.client != nil && f.client.IsConnected() {
		f.client.Disconnect(250)
	}
}

