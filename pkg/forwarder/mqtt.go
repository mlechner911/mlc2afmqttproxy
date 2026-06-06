package forwarder

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

type MQTTForwarder struct {
	Upstream      string
	TimestampMode string
	client        paho.Client
}

// NewMQTTForwarder erstellt einen neuen Paho MQTT Client für den Cloud-Broker.
func NewMQTTForwarder(upstream, username, password, timestampMode string) *MQTTForwarder {
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
		Upstream:      upstream,
		TimestampMode: timestampMode,
		client:        paho.NewClient(opts),
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

	finalPayload := payload

	if f.TimestampMode == "json_inject" {
		var raw map[string]interface{}
		if err := json.Unmarshal(payload, &raw); err == nil {
			raw["ts"] = timestamp.UnixMilli() // You can also format as string if preferred, but usually ms timestamp is used
			if injected, err := json.Marshal(raw); err == nil {
				finalPayload = injected
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
