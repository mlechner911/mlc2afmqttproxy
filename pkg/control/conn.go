package control

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"sync/atomic"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
)

// conn ist eine MQTT-5-Verbindung OHNE gespeicherte Sitzung: CleanStart bei jedem Aufbau,
// Sitzung endet mit der Verbindung. Ein Befehl, der während eines Ausfalls aufläuft, kommt so nie
// verspätet an (der Forwarder dagegen behält seine Sitzung, damit keine Messwerte verloren gehen).
type conn struct {
	name   string
	cm     *autopaho.ConnectionManager
	online atomic.Bool
}

// connOpts beschreibt eine Verbindung: Ziel, Zugang, Client-ID, Abos und Empfang.
type connOpts struct {
	name, server, clientID, username, password string
	subscribe                                  []string
	onMessage                                  func(autopaho.PublishReceived)
}

func dial(ctx context.Context, o connOpts) (*conn, error) {
	u, err := url.Parse(o.server)
	if err != nil {
		return nil, fmt.Errorf("%s: ungültige Broker-URL %q: %w", o.name, o.server, err)
	}
	c := &conn{name: o.name}
	cfg := autopaho.ClientConfig{
		ServerUrls:                    []*url.URL{u},
		KeepAlive:                     30,
		CleanStartOnInitialConnection: true,
		SessionExpiryInterval:         0,
		OnConnectionUp: func(cm *autopaho.ConnectionManager, _ *paho.Connack) {
			c.online.Store(true)
			c.subscribeAll(ctx, cm, o.subscribe)
		},
		OnConnectionDown: func() bool { c.online.Store(false); return true },
		OnConnectError: func(err error) {
			c.online.Store(false)
			log.Printf("[control %s] Verbindung zu %s fehlgeschlagen: %v", o.name, o.server, err)
		},
		// Empfang über ClientConfig: gilt für JEDE Verbindung. AddOnPublishReceived hängt nur an die
		// gerade bestehende — vor dem ersten Aufbau und nach jedem Wiederverbinden ging er verloren.
		ClientConfig: paho.ClientConfig{ClientID: o.clientID, OnPublishReceived: []func(paho.PublishReceived) (bool, error){
			func(pr paho.PublishReceived) (bool, error) {
				if o.onMessage != nil {
					o.onMessage(autopaho.PublishReceived{PublishReceived: pr})
				}
				return true, nil
			},
		}},
	}
	// Jede neue Verbindung beginnt leer (autopaho setzt CleanStart sonst nur beim ersten Aufbau).
	// Zugang hier statt über SetUsernamePassword — das ersetzt den ganzen ConnectPacketBuilder.
	cfg.ConnectPacketBuilder = func(cp *paho.Connect, _ *url.URL) (*paho.Connect, error) {
		cp.CleanStart = true
		if o.username != "" {
			cp.Username, cp.UsernameFlag = o.username, true
			cp.Password, cp.PasswordFlag = []byte(o.password), true
		}
		return cp, nil
	}
	cm, err := autopaho.NewConnection(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", o.name, err)
	}
	c.cm = cm
	return c, nil
}

// subscribeAll abonniert nach jedem Verbindungsaufbau; ein abgelehntes Abo (Reason ≥ 0x80, z. B.
// ACL im Monitor noch nicht eingerichtet) wird laut gemeldet statt still hingenommen. NoLocal: die
// eigenen Quittungen kommen unter einem Abo wie cmd/zb/# nicht zurück.
func (c *conn) subscribeAll(ctx context.Context, cm *autopaho.ConnectionManager, topics []string) {
	for _, t := range topics {
		sa, err := cm.Subscribe(ctx, &paho.Subscribe{Subscriptions: []paho.SubscribeOptions{{Topic: t, QoS: 1, NoLocal: true}}})
		switch {
		case sa != nil && len(sa.Reasons) > 0 && sa.Reasons[0] >= 0x80:
			// paho meldet das auch als err, aber ohne Code — der Code sagt, warum (0x87 = keine Berechtigung).
			log.Printf("[control %s] Abo %s vom Broker ABGELEHNT (0x%02x) — Befehle kommen nicht an", c.name, t, sa.Reasons[0])
		case err != nil:
			log.Printf("[control %s] Abo %s fehlgeschlagen: %v", c.name, t, err)
		default:
			log.Printf("[control %s] abonniert: %s", c.name, t)
		}
	}
}

func (c *conn) publish(ctx context.Context, topic string, payload []byte) error {
	if c.cm == nil || !c.online.Load() {
		return fmt.Errorf("%s: nicht verbunden", c.name)
	}
	_, err := c.cm.Publish(ctx, &paho.Publish{Topic: topic, QoS: 1, Payload: payload})
	return err
}

func (c *conn) close(ctx context.Context) {
	if c != nil && c.cm != nil {
		_ = c.cm.Disconnect(ctx)
	}
}
