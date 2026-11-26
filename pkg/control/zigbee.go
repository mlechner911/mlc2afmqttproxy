package control

import (
	"context"

	"github.com/eclipse/paho.golang/autopaho"
)

// FuncZigbee ist ein Weg zu zigbee2mqtt aus zwei Funktionen (Direktbetrieb: eigener Broker; die
// Zustandsmeldungen kommen dort über den Beobachter am Broker).
type FuncZigbee struct {
	PublishFn func(topic string, payload []byte) error
	ReadyFn   func() bool
}

// Publish veröffentlicht über PublishFn.
func (f FuncZigbee) Publish(_ context.Context, topic string, payload []byte) error {
	return f.PublishFn(topic, payload)
}

// Ready meldet, ob zigbee2mqtt erreichbar ist.
func (f FuncZigbee) Ready() bool { return f.ReadyFn() }

// BrokerZigbee ist der Weg über einen fremden Broker (z. B. Mosquitto, an dem zigbee2mqtt hängt).
// Er abonniert dort <base>/+ und füttert die Wartenden mit den Zustandsmeldungen — unabhängig
// davon, ob die mqttbridge zum eigenen Broker läuft.
type BrokerZigbee struct{ c *conn }

// DialZigbee verbindet mit dem Broker von zigbee2mqtt.
func DialZigbee(ctx context.Context, server, clientID, username, password, base string, w *Waiters) (*BrokerZigbee, error) {
	c, err := dial(ctx, connOpts{
		name: "zigbee2mqtt", server: server, clientID: clientID, username: username, password: password,
		subscribe: []string{base + "/+"},
		onMessage: func(pr autopaho.PublishReceived) { w.Observe(pr.Packet.Topic, pr.Packet.Payload) },
	})
	if err != nil {
		return nil, err
	}
	return &BrokerZigbee{c: c}, nil
}

// Publish schickt den Befehl an zigbee2mqtt.
func (b *BrokerZigbee) Publish(ctx context.Context, topic string, payload []byte) error {
	return b.c.publish(ctx, topic, payload)
}

// Ready meldet, ob die Verbindung zum Broker von zigbee2mqtt steht.
func (b *BrokerZigbee) Ready() bool { return b.c.online.Load() }

// Close trennt.
func (b *BrokerZigbee) Close(ctx context.Context) { b.c.close(ctx) }
