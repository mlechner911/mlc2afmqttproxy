package control

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/eclipse/paho.golang/autopaho"
	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/mochi-mqtt/server/v2/packets"
)

func testBroker(t *testing.T) (string, *mqtt.Server) {
	t.Helper()
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	s := mqtt.New(&mqtt.Options{InlineClient: true})
	_ = s.AddHook(new(auth.AllowHook), nil)
	addr := "127.0.0.1:" + strconv.Itoa(port)
	if err := s.AddListener(listeners.NewTCP(listeners.Config{ID: "t", Address: addr})); err != nil {
		t.Fatal(err)
	}
	go func() { _ = s.Serve() }()
	t.Cleanup(func() { _ = s.Close() })
	return "tcp://" + addr, s
}

func TestConnReceives(t *testing.T) {
	url, s := testBroker(t)
	got := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := dial(ctx, connOpts{name: "t", server: url, clientID: "c1", subscribe: []string{"cmd/zb/1/+/set"},
		onMessage: func(pr autopaho.PublishReceived) { got <- pr.Packet.Topic }})
	if err != nil {
		t.Fatal(err)
	}
	defer c.close(ctx)
	deadline := time.Now().Add(3 * time.Second)
	for !c.online.Load() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	_ = s.Publish("cmd/zb/1/0x01/set", []byte(`{}`), false, 1)
	select {
	case tp := <-got:
		t.Logf("empfangen: %s", tp)
	case <-time.After(3 * time.Second):
		t.Fatal("nichts empfangen")
	}
}

// TestConnReceivesAfterReconnect: Nach einem Wiederverbinden kommt weiter alles an (der Handler
// hing früher nur an der ersten Verbindung).
func TestConnReceivesAfterReconnect(t *testing.T) {
	url, s := testBroker(t)
	got := make(chan string, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := dial(ctx, connOpts{name: "t", server: url, clientID: "c2", subscribe: []string{"x/#"},
		onMessage: func(pr autopaho.PublishReceived) { got <- pr.Packet.Topic }})
	if err != nil {
		t.Fatal(err)
	}
	defer c.close(ctx)
	waitOnline := func() {
		deadline := time.Now().Add(5 * time.Second)
		for !c.online.Load() && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		time.Sleep(200 * time.Millisecond)
	}
	waitOnline()
	if cl, ok := s.Clients.Get("c2"); ok {
		s.DisconnectClient(cl, packets.ErrSessionTakenOver) // Verbindung vom Broker aus trennen
	}
	time.Sleep(300 * time.Millisecond)
	waitOnline()
	_ = s.Publish("x/1", []byte(`{}`), false, 1)
	select {
	case <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("nach dem Wiederverbinden nichts empfangen")
	}
}
