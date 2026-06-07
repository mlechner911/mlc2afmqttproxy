package forwarder

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
	"mlc2afmqttproxy/pkg/config"
)

// MQTT5Forwarder implementiert die Forwarder-Schnittstelle unter Verwendung von MQTT v5 (via Eclipse Paho v5/autopaho).
// Er übermittelt Nachrichten an einen Upstream-Master/Cloud-Broker und sendet den historischen Zeitstempel
// als MQTT v5 User Property ("ts"), um Payload-Injektionen zu vermeiden und das JSON unberührt zu lassen.
type MQTT5Forwarder struct {
	// Upstream ist die Broker-URL (z.B. tcp://cloud.example.com:1883)
	Upstream string
	// Rewrite enthält Umschreibregeln für das Topic
	Rewrite  *config.TopicRewriteConf
	// client ist der Autopaho Connection Manager für automatische Verbindungsaufrechterhaltung
	client   *autopaho.ConnectionManager
	// ctx ist der Kontext für asynchrone Client-Prozesse
	ctx      context.Context
	// cancel bricht den asynchronen Client-Prozess bei Schließen ab
	cancel   context.CancelFunc
}

// NewMQTT5Forwarder erstellt und konfiguriert einen neuen MQTT5Forwarder für Upstream-MQTT v5.
// Konfiguriert den autopaho Connection Manager zur automatischen Wiederverbindung im Hintergrund.
func NewMQTT5Forwarder(upstream, username, password string, rewrite *config.TopicRewriteConf) *MQTT5Forwarder {
	ctx, cancel := context.WithCancel(context.Background())

	u, err := url.Parse(upstream)
	if err != nil {
		log.Printf("[MQTT5-Upstream] Ungültige Upstream-URL: %v", err)
	}

	cliCfg := autopaho.ClientConfig{
		BrokerUrls: []*url.URL{u},
		KeepAlive:  30,
		OnConnectionUp: func(cm *autopaho.ConnectionManager, connAck *paho.Connack) {
			log.Printf("[MQTT5-Upstream] Erfolgreich verbunden mit %s", upstream)
		},
		OnConnectError: func(err error) {
			log.Printf("[MQTT5-Upstream] Verbindung fehlgeschlagen: %v", err)
		},
		ClientConfig: paho.ClientConfig{
			ClientID: "mlc2af-proxy-forwarder-v5",
		},
	}

	if username != "" {
		cliCfg.ClientConfig.Router = paho.NewStandardRouter()
		cliCfg.SetUsernamePassword(username, []byte(password))
	}

	cm, err := autopaho.NewConnection(ctx, cliCfg)
	if err != nil {
		log.Printf("[MQTT5-Upstream] Fehler beim Erstellen der Verbindung: %v", err)
	}

	return &MQTT5Forwarder{
		Upstream: upstream,
		Rewrite:  rewrite,
		client:   cm,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Connect implementiert das Forwarder-Interface. Da autopaho asynchron arbeitet, ist keine blockierende Aktion nötig.
func (f *MQTT5Forwarder) Connect() error {
	if f.client == nil {
		return fmt.Errorf("client not initialized")
	}
	// autopaho verbindet sich automatisch im Hintergrund
	return nil
}

// IsConnected prüft, ob der Connection Manager initialisiert ist.
func (f *MQTT5Forwarder) IsConnected() bool {
	if f.client == nil {
		return false
	}
	return true 
}

// Send führt optional Topic-Umschreibungen aus, packt den historischen Zeitstempel
// als MQTT v5 User Property ("ts") in die Nachricht und veröffentlicht diese mit QoS 1 (At least once).
func (f *MQTT5Forwarder) Send(topic string, payload []byte, timestamp time.Time) error {
	if f.client == nil {
		return fmt.Errorf("upstream mqtt client is not connected")
	}

	// 1. Topic-Umschreibung (z.B. "zigbee2mqtt/sensor1" -> "cloud/sensor1")
	if f.Rewrite != nil && f.Rewrite.MatchPrefix != "" {
		if strings.HasPrefix(topic, f.Rewrite.MatchPrefix) {
			topic = f.Rewrite.ReplaceWith + strings.TrimPrefix(topic, f.Rewrite.MatchPrefix)
		}
	}

	// Zeitstempel als RFC3339 String im Header (User Property)
	tsStr := timestamp.UTC().Format(time.RFC3339)

	pb := &paho.Publish{
		Topic:   topic,
		QoS:     1,
		Retain:  false,
		Payload: payload,
		Properties: &paho.PublishProperties{
			User: paho.UserProperties{
				// ts-Property anhängen zur korrekten zeitlichen Zuordnung
				paho.UserProperty{Key: "ts", Value: tsStr},
			},
		},
	}

	// Blockierendes Veröffentlichen
	pubResp, err := f.client.Publish(f.ctx, pb)
	if err != nil {
		return err
	}
	
	if pubResp != nil && pubResp.ReasonCode != 0 {
		return fmt.Errorf("publish failed with reason code: %d", pubResp.ReasonCode)
	}

	log.Printf("[MQTT5-Upstream] Erfolgreich gesendet: Topic='%s', %d bytes (mit ts=%s)", topic, len(payload), tsStr)
	return nil
}

// Close trennt den MQTT 5 Client sauber ab und bricht den internen Kontext ab.
func (f *MQTT5Forwarder) Close() {
	if f.client != nil {
		f.client.Disconnect(f.ctx)
	}
	if f.cancel != nil {
		f.cancel()
	}
}

