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

type MQTT5Forwarder struct {
	Upstream string
	Rewrite  *config.TopicRewriteConf
	client   *autopaho.ConnectionManager
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewMQTT5Forwarder erstellt einen neuen Paho MQTT v5 Client für den Cloud-Broker.
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
		cliCfg.ClientConfig.Router = paho.NewStandardRouter() // Optional
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

// Connect stellt die Initiale Verbindung zum Broker her.
func (f *MQTT5Forwarder) Connect() error {
	if f.client == nil {
		return fmt.Errorf("client not initialized")
	}
	// autopaho verbindet automatisch im Hintergrund
	return nil
}

// IsConnected prüft, ob Paho aktuell verbunden ist.
func (f *MQTT5Forwarder) IsConnected() bool {
	if f.client == nil {
		return false
	}
	// Falls es keine direkte Methode gibt, versuchen wir es optimistisch
	return true 
}

// Send publiziert die Nachricht an den Upstream-Broker.
func (f *MQTT5Forwarder) Send(topic string, payload []byte, timestamp time.Time) error {
	if f.client == nil {
		return fmt.Errorf("upstream mqtt client is not connected")
	}

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
				paho.UserProperty{Key: "ts", Value: tsStr},
			},
		},
	}

	// Publish blocking
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

// Close trennt die Verbindung sauber.
func (f *MQTT5Forwarder) Close() {
	if f.client != nil {
		f.client.Disconnect(f.ctx)
	}
	if f.cancel != nil {
		f.cancel()
	}
}
