package forwarder

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
	"github.com/eclipse/paho.golang/paho/extensions/topicaliases"
	"mlc2afmqttproxy/pkg/config"
)

// MQTT5Forwarder implementiert die Forwarder-Schnittstelle unter Verwendung von MQTT v5 (via Eclipse Paho v5/autopaho).
// Er übermittelt Nachrichten an einen Upstream-Master/Cloud-Broker und sendet den historischen Zeitstempel
// als MQTT v5 User Property ("ts"), um Payload-Injektionen zu vermeiden und das JSON unberührt zu lassen.
// Unterstützt Downstream: Subscribe vom Upstream-Broker und Forward an lokale Clients.
type MQTT5Forwarder struct {
	// Upstream ist die Broker-URL (z.B. tcp://cloud.example.com:1883)
	Upstream string
	// Rewrite enthält Umschreibregeln für das Topic (Upstream-Richtung)
	Rewrite *config.TopicRewriteConf
	// client ist der Autopaho Connection Manager für automatische Verbindungsaufrechterhaltung
	client *autopaho.ConnectionManager
	// ctx ist der Kontext für asynchrone Client-Prozesse
	ctx context.Context
	// cancel bricht den asynchronen Client-Prozess bei Schließen ab
	cancel context.CancelFunc

	// Konstruktionsparameter (für Reconnect-Zwecke)
	username       string
	password       string
	enableTopicAlias bool

	// Downstream
	downHandler           DownstreamHandler
	downSubscribed        bool
	downTopics            []string
	downRewrite           *config.TopicRewriteConf
	downMu                sync.Mutex
	downHandlerRegistered bool
}

// NewMQTT5Forwarder erstellt und konfiguriert einen neuen MQTT5Forwarder für Upstream-MQTT v5.
// Die Verbindung wird erst bei Connect() aufgebaut — so kann Subscribe() davor konfiguriert werden.
func NewMQTT5Forwarder(upstream, username, password string, enableTopicAlias bool, rewrite *config.TopicRewriteConf) *MQTT5Forwarder {
	ctx, cancel := context.WithCancel(context.Background())

	return &MQTT5Forwarder{
		Upstream:         upstream,
		Rewrite:          rewrite,
		ctx:              ctx,
		cancel:           cancel,
		username:         username,
		password:         password,
		enableTopicAlias: enableTopicAlias,
	}
}

// connectClient erstellt die Connection zum Upstream-Broker.
func (f *MQTT5Forwarder) connectClient() error {
	u, err := url.Parse(f.Upstream)
	if err != nil {
		return fmt.Errorf("ungültige Upstream-URL: %v", err)
	}

	cliCfg := autopaho.ClientConfig{
		BrokerUrls: []*url.URL{u},
		KeepAlive:  30,
		OnConnectError: func(err error) {
			log.Printf("[MQTT5-Upstream] Verbindung fehlgeschlagen: %v", err)
			},
		ClientConfig: paho.ClientConfig{
			ClientID: "mlc2af-proxy-forwarder-v5",
			},
	}

	if f.username != "" {
		cliCfg.ClientConfig.Router = paho.NewStandardRouter()
		cliCfg.SetUsernamePassword(f.username, []byte(f.password))
	}

	var taHandler *topicaliases.TAHandler
	if f.enableTopicAlias {
		taHandler = topicaliases.NewTAHandler(64) // Ausreichend für Zigbee (spart massiv Bandbreite)
		cliCfg.ClientConfig.PublishHook = taHandler.PublishHook
	}

	cliCfg.OnConnectionUp = func(cm *autopaho.ConnectionManager, connAck *paho.Connack) {
		log.Printf("[MQTT5-Upstream] Erfolgreich verbunden mit %s", f.Upstream)
		if taHandler != nil {
			taHandler.ResetAll() // Wichtig für Server-Restarts, damit die Aliases beim neuen Connect neu verhandelt werden!
			log.Printf("[MQTT5-Upstream] Topic Aliases für neue Session zurückgesetzt.")
			}

			// Downstream Publish-Handler registrieren (einmalig)
		f.downMu.Lock()
		defer f.downMu.Unlock()
		if f.downHandler != nil && !f.downHandlerRegistered {
			cm.AddOnPublishReceived(f.handlePublishReceived)
			f.downHandlerRegistered = true
			log.Printf("[MQTT5-Downstream] Publish-Handler registriert")
			}

			// Downstream Subscribe bei Connect/Reconnect
		if f.downTopics != nil && !f.downSubscribed {
			for _, topic := range f.downTopics {
				if sub, err := cm.Subscribe(f.ctx, &paho.Subscribe{
					Subscriptions: []paho.SubscribeOptions{
						{Topic: topic, QoS: 1},
					},
					}); err != nil {
					log.Printf("[MQTT5-Downstream] Subscribe für '%s' fehlgeschlagen: %v", topic, err)
					} else {
					log.Printf("[MQTT5-Downstream] subscribed: %v", sub)
					}
				}
			f.downSubscribed = true
			}
	}

	cm, err := autopaho.NewConnection(f.ctx, cliCfg)
	if err != nil {
		return fmt.Errorf("Fehler beim Erstellen der Verbindung: %v", err)
	}
	f.client = cm

	return nil
}

// Connect baut die Verbindung zum Upstream-Broker auf (lazys Initialisierung).
func (f *MQTT5Forwarder) Connect() error {
	if f.client != nil {
		return nil // Bereits verbunden
	}
	return f.connectClient()
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
// Setzt zusätzlich das User Property "origin=local" zur Loop-Detection.
func (f *MQTT5Forwarder) Send(topic string, payload []byte, timestamp time.Time) error {
	if f.client == nil {
		return fmt.Errorf("upstream mqtt client is not connected")
	}

	// Vorabprüfung auf Wildcards (verboten beim Publizieren)
	if strings.ContainsAny(topic, "+#") {
		return &PermanentError{Err: fmt.Errorf("invalid topic contains wildcard: %s", topic)}
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
					// origin="local" markiert Upstream-Nachrichten zur Loop-Detection
				paho.UserProperty{Key: "origin", Value: "local"},
				},
			},
	}

	// Blockierendes Veröffentlichen
	pubResp, err := f.client.Publish(f.ctx, pb)
	if err != nil {
		return err
	}

	if pubResp != nil && pubResp.ReasonCode != 0 {
		return &PermanentError{Err: fmt.Errorf("publish failed with reason code: %d", pubResp.ReasonCode)}
	}

	log.Printf("[MQTT5-Upstream] Erfolgreich gesendet: Topic='%s', %d bytes (mit ts=%s)", topic, len(payload), tsStr)
	return nil
}

// Subscribe registriert Topics und einen Handler für eingehende Nachrichten vom Upstream-Broker.
// Muss VOR Connect() aufgerufen werden, damit der Handler beim Verbindungsaufbau registriert wird.
func (f *MQTT5Forwarder) Subscribe(topics []string, handler DownstreamHandler) error {
	if len(topics) == 0 {
		return nil
	}

	f.downMu.Lock()
	defer f.downMu.Unlock()

	f.downTopics = topics
	f.downHandler = handler
	// downSubscribed bleibt false → wird via OnConnectionUp beim Connect gesetzt
	// downHandlerRegistered bleibt false → wird via OnConnectionUp beim Connect gesetzt (einmalig)

	// Falls bereits verbunden, direkt subscriben
	if f.client != nil && f.downSubscribed {
		f.downSubscribed = false // Reset, damit OnConnectUp beim nächsten Reconnect neu subscribt
			for _, topic := range f.downTopics {
				if sub, err := f.client.Subscribe(f.ctx, &paho.Subscribe{
					Subscriptions: []paho.SubscribeOptions{
						{Topic: topic, QoS: 1},
						},
					}); err != nil {
					log.Printf("[MQTT5-Downstream] Subscribe für '%s' fehlgeschlagen: %v", topic, err)
					} else {
					log.Printf("[MQTT5-Downstream] subscribed: %v", sub)
					}
				}
			f.downSubscribed = true
			log.Printf("[MQTT5-Downstream] bereits verbunden — Subscribe direkt")
		}

	return nil
}

// SetDownRewrite setzt die Topic-Umschreibregel für Downstream-Nachrichten.
func (f *MQTT5Forwarder) SetDownRewrite(rewrite *config.TopicRewriteConf) {
	f.downMu.Lock()
	defer f.downMu.Unlock()
	f.downRewrite = rewrite
}

// handlePublishReceived verarbeitet eingehende Nachrichten vom Upstream-Broker.
// Nachrichten mit origin="local" werden verworfen (Loop-Detection).
func (f *MQTT5Forwarder) handlePublishReceived(pr autopaho.PublishReceived) (bool, error) {
	// Loop-Detection: Verwerfe eigene Nachrichten (origin="local")
	if pr.Packet.Properties != nil {
		for _, prop := range pr.Packet.Properties.User {
			if prop.Key == "origin" && prop.Value == "local" {
				return true, nil // Eigene Nachricht → ignorieren
				}
			}
	}

	topic := pr.Packet.Topic
	payload := pr.Packet.Payload

	// Topic-Umschreibung (z.B. "cloud/commands/zigbee2mqtt/+" -> "zigbee2mqtt/+")
	if f.downRewrite != nil && f.downRewrite.MatchPrefix != "" {
		if strings.HasPrefix(topic, f.downRewrite.MatchPrefix) {
			topic = f.downRewrite.ReplaceWith + strings.TrimPrefix(topic, f.downRewrite.MatchPrefix)
			}
	}

	log.Printf("[MQTT5-Downstream] Empfangen: Topic='%s', %d bytes", topic, len(payload))

	if f.downHandler != nil {
		f.downHandler(topic, payload)
	}

	return true, nil
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

// SetDownstreamHandler setzt den Callback für Downstream-Nachrichten.
func (f *MQTT5Forwarder) SetDownstreamHandler(h DownstreamHandler) {
	f.downMu.Lock()
	defer f.downMu.Unlock()
	f.downHandler = h
}
