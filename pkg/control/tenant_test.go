package control

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
)

// TestTenantAckOnCommandTopic: Abo cmd/zb/# ohne Kundennummer; die Quittung geht auf das
// empfangene Topic mit /ack, und die eigene Quittung wird nicht als Befehl gelesen.
func TestTenantAckOnCommandTopic(t *testing.T) {
	url, s := testBroker(t)
	acks := make(chan packets.Packet, 4)
	if err := s.Subscribe("cmd/zb/#", 1, func(_ *mqtt.Client, _ packets.Subscription, pk packets.Packet) {
		if IsCommand(pk.TopicName) {
			return
		}
		acks <- pk
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := NewWaiters("zigbee2mqtt")
	j := NewJournal()
	zb := FuncZigbee{
		ReadyFn: func() bool { return true },
		PublishFn: func(topic string, payload []byte) error {
			// zigbee2mqtt meldet den neuen Zustand sofort
			go w.Observe("zigbee2mqtt/"+plug.Name, payload)
			return nil
		},
	}
	tn, err := StartTenant(ctx, TenantOpts{Name: "main", Base: "zigbee2mqtt", Server: url, ClientID: "box-zb",
		Allow: map[string][]string{plug.IEEE: {"state"}}, Lookup: lookup, Timeout: 2 * time.Second,
		Zigbee: zb, Waiters: w, Journal: j})
	if err != nil {
		t.Fatal(err)
	}
	defer tn.Close(ctx)
	if tn.Subscribe() != DefaultSubscribe {
		t.Fatalf("Abo = %q, erwartet %q", tn.Subscribe(), DefaultSubscribe)
	}
	deadline := time.Now().Add(3 * time.Second)
	for !tn.up.online.Load() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)

	_ = s.Publish("cmd/zb/4111/"+plug.IEEE+"/set", []byte(`{"state":"ON","_id":"u1"}`), false, 1)
	select {
	case pk := <-acks:
		if want := "cmd/zb/4111/" + plug.IEEE + "/ack"; pk.TopicName != want {
			t.Fatalf("Quittung auf %q, erwartet %q", pk.TopicName, want)
		}
		var a Ack
		if err := json.Unmarshal(pk.Payload, &a); err != nil || a.ID != "u1" || a.Result != "ok" {
			t.Fatalf("Quittung %s (%v)", pk.Payload, err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("keine Quittung")
	}
	time.Sleep(300 * time.Millisecond)
	if c := j.Counts(); len(c) != 1 || c["ok"] != 1 {
		t.Fatalf("Zähler %v — die eigene Quittung darf kein Befehl sein", c)
	}
}
