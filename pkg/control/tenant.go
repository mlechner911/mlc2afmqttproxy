package control

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/eclipse/paho.golang/autopaho"
	"mlc2afmqttproxy/pkg/devices"
)

// Zigbee ist der Weg zu zigbee2mqtt: der eigene Broker (Direktbetrieb) oder der Broker, an dem
// zigbee2mqtt hängt (z. B. Mosquitto auf 1883).
type Zigbee interface {
	Publish(ctx context.Context, topic string, payload []byte) error
	Ready() bool
}

// DefaultSubscribe ist das Befehls-Abo, wenn die Konfiguration keines nennt. mlcmonit stellt
// darüber nur Befehle des eigenen Mandanten zu (Prüfung je Zustellung im Broker).
const DefaultSubscribe = "cmd/zb/#"

// TenantOpts beschreibt einen Mandanten: Haupt-Upstream oder eine Route.
type TenantOpts struct {
	Name, Subscribe, Base                string
	Server, ClientID, Username, Password string
	Allow                                map[string][]string
	Lookup                               func(ieee string) (devices.Device, bool)
	PerMinute                            int
	Timeout                              time.Duration
	Zigbee                               Zigbee
	Waiters                              *Waiters
	Journal                              *Journal
}

// Tenant nimmt Befehle eines Mandanten an und quittiert sie.
type Tenant struct {
	o      TenantOpts
	policy *Policy
	up     *conn
}

// StartTenant verbindet mit dem Upstream des Mandanten und abonniert o.Subscribe.
func StartTenant(ctx context.Context, o TenantOpts) (*Tenant, error) {
	if o.Timeout <= 0 {
		o.Timeout = 10 * time.Second
	}
	if o.Subscribe == "" {
		o.Subscribe = DefaultSubscribe
	}
	t := &Tenant{o: o, policy: NewPolicy(o.Allow, o.Lookup, o.PerMinute)}
	up, err := dial(ctx, connOpts{
		name: o.Name, server: o.Server, clientID: o.ClientID, username: o.Username, password: o.Password,
		subscribe: []string{o.Subscribe},
		onMessage: func(pr autopaho.PublishReceived) { t.handle(ctx, pr) },
	})
	if err != nil {
		return nil, err
	}
	t.up = up
	log.Printf("[control %s] aktiv: %d Gerät(e) freigegeben, Befehle auf %s", o.Name, len(o.Allow), o.Subscribe)
	return t, nil
}

// Subscribe liefert das Befehls-Abo dieses Mandanten.
func (t *Tenant) Subscribe() string { return t.o.Subscribe }

// Allowed liefert die Freigabe dieses Mandanten.
func (t *Tenant) Allowed() map[string][]string { return t.policy.Allowed() }

// Close trennt die Upstream-Verbindung.
func (t *Tenant) Close(ctx context.Context) { t.up.close(ctx) }

// Ack ist die Quittung an den Monitor.
type Ack struct {
	ID     string         `json:"id"`
	Result string         `json:"result" example:"ok"` // ok | rejected | timeout
	Reason string         `json:"reason,omitempty"`
	State  map[string]any `json:"state,omitempty"`
}

func (t *Tenant) handle(ctx context.Context, pr autopaho.PublishReceived) {
	pk := pr.Packet
	if !IsCommand(pk.Topic) {
		return // z. B. die eigene Quittung unter cmd/zb/#
	}
	if len(pk.Payload) == 0 {
		return // leer = Löschen einer gespeicherten Nachricht, kein Befehl
	}
	if pk.Retain {
		// Ein gespeicherter Befehl käme bei jeder Verbindung erneut — nie ausführen, nicht quittieren.
		log.Printf("[control %s] gespeicherten (retained) Befehl auf %s verworfen", t.o.Name, pk.Topic)
		return
	}
	req, reason := ParseRequest(pk.Topic, pk.Payload)
	if reason == "" {
		var dev devices.Device
		dev, reason = t.policy.Check(req, time.Now())
		if reason == "" {
			reason = t.forward(ctx, req, dev)
		}
	}
	if reason != "" {
		t.finish(ctx, req, "", Ack{ID: req.ID, Result: "rejected", Reason: reason})
	}
}

// forward reicht den Befehl an zigbee2mqtt und wartet nebenläufig auf die Zustandsmeldung.
func (t *Tenant) forward(ctx context.Context, req Request, dev devices.Device) string {
	if !t.o.Zigbee.Ready() {
		return ReasonNoZigbee
	}
	payload, _ := json.Marshal(req.Fields)
	done, cancel := t.o.Waiters.Add(dev.Name, req.Fields)
	if err := t.o.Zigbee.Publish(ctx, t.o.Base+"/"+dev.Name+"/set", payload); err != nil {
		cancel()
		log.Printf("[control %s] an zigbee2mqtt fehlgeschlagen: %v", t.o.Name, err)
		return ReasonNoZigbee
	}
	go func() {
		timer := time.NewTimer(t.o.Timeout)
		defer timer.Stop()
		select {
		case st := <-done:
			cancel()
			t.finish(ctx, req, dev.Name, Ack{ID: req.ID, Result: "ok", State: pick(st, req.Fields)})
		case <-timer.C:
			last := cancel()
			t.finish(ctx, req, dev.Name, Ack{ID: req.ID, Result: "timeout",
				Reason: fmt.Sprintf("no matching state report within %s", t.o.Timeout), State: pick(last, req.Fields)})
		case <-ctx.Done():
			cancel()
		}
	}()
	return ""
}

// finish quittiert, protokolliert und zählt.
func (t *Tenant) finish(ctx context.Context, req Request, name string, ack Ack) {
	t.o.Journal.Add(Entry{Time: time.Now(), Tenant: t.o.Name, IEEE: req.IEEE, Device: name,
		ID: req.ID, Fields: req.Fields, Result: ack.Result, Reason: ack.Reason})
	log.Printf("[control %s] %s %v → %s %s", t.o.Name, req.IEEE, req.Fields, ack.Result, ack.Reason)
	if req.AckTopic == "" {
		return // kein Gerät im Topic: keine Quittungsadresse
	}
	b, _ := json.Marshal(ack)
	if err := t.up.publish(ctx, req.AckTopic, b); err != nil {
		log.Printf("[control %s] Quittung nicht gesendet: %v", t.o.Name, err)
	}
}

// pick liefert aus dem gemeldeten Zustand nur die angefragten Felder (die Quittung bleibt klein).
func pick(state, want map[string]any) map[string]any {
	if state == nil {
		return nil
	}
	out := map[string]any{}
	for k := range want {
		if v, ok := state[k]; ok {
			out[k] = v
		}
	}
	return out
}
