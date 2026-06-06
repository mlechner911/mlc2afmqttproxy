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

type MQTTForwarder struct {
	Upstream       string
	TimestampMode  string
	TimestampField string
	Rewrite        *config.TopicRewriteConf
	client         paho.Client
}

// NewMQTTForwarder erstellt einen neuen Paho MQTT Client für den Cloud-Broker.
func NewMQTTForwarder(upstream, username, password, timestampMode, timestampField string, rewrite *config.TopicRewriteConf) *MQTTForwarder {
	opts := paho.NewClientOptions()
	opts.AddBroker(upstream)
	opts.SetClientID("mlc2af-proxy-forwarder") // Falls nötig, kann dies über config dynamisiert werden
	
	if username != "" {
		opts.SetUsername(username)
	}
	if password != "" {
		opts.SetPassword(password)
	}

	// AutoReconnect übernimmt Paho intern, wir fangen aber Disconnects ab für sauberes Logging
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

// Connect stellt die Initiale Verbindung zum Broker her.
func (f *MQTTForwarder) Connect() error {
	token := f.client.Connect()
	token.Wait()
	if token.Error() != nil {
		return token.Error()
	}
	return nil
}

// IsConnected prüft, ob Paho aktuell verbunden ist.
func (f *MQTTForwarder) IsConnected() bool {
	if f.client == nil {
		return false
	}
	return f.client.IsConnectionOpen() || f.client.IsConnected()
}

// Send publiziert die Nachricht (mit QoS 1) an den Upstream-Broker.
func (f *MQTTForwarder) Send(topic string, payload []byte, timestamp time.Time) error {
	if !f.IsConnected() {
		return fmt.Errorf("upstream mqtt client is not connected")
	}

	if f.Rewrite != nil && f.Rewrite.MatchPrefix != "" {
		if strings.HasPrefix(topic, f.Rewrite.MatchPrefix) {
			topic = f.Rewrite.ReplaceWith + strings.TrimPrefix(topic, f.Rewrite.MatchPrefix)
		}
	}

	finalPayload := payload

	if f.TimestampMode == "json_inject" {
		var data map[string]interface{}
		// Versuche Payload als JSON zu parsen
		if err := json.Unmarshal(payload, &data); err == nil {
			// "Inject if absent": überschreibe niemals einen existierenden Wert
			if _, exists := data[f.TimestampField]; !exists {
				data[f.TimestampField] = timestamp.UnixMilli()
				if newPayload, err := json.Marshal(data); err == nil {
					finalPayload = newPayload
				}
			}
		}
	}

	// QoS 1 (Mindestens einmal), Retained=false
	token := f.client.Publish(topic, 1, false, finalPayload)
	token.Wait()
	
	if token.Error() != nil {
		return token.Error()
	}

	log.Printf("[MQTT-Upstream] Erfolgreich gesendet: Topic='%s', %d bytes", topic, len(finalPayload))
	return nil
}

// Close trennt die Verbindung sauber.
func (f *MQTTForwarder) Close() {
	if f.client != nil && f.client.IsConnected() {
		f.client.Disconnect(250)
	}
}
