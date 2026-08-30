package forwarder

import (
	"testing"
	"time"
)

func TestHTTPForwarder_Subscribe_NoOp(t *testing.T) {
	f := NewHTTPForwarder("http://example.com/ingest", "test-token")

	err := f.Subscribe([]string{"cloud/commands/+"}, func(topic string, payload []byte) {})
	if err != nil {
		t.Errorf("Subscribe should return nil, got: %v", err)
	}

	// SetDownstreamHandler sollte No-Op sein
	f.SetDownstreamHandler(func(topic string, payload []byte) {})
}

func TestHTTPForwarder_Subscribe_EmptyTopics(t *testing.T) {
	f := NewHTTPForwarder("http://example.com/ingest", "test-token")

	err := f.Subscribe([]string{}, func(topic string, payload []byte) {})
	if err != nil {
		t.Errorf("Subscribe with empty topics should return nil, got: %v", err)
	}
}

func TestHTTPForwarder_IsConnected(t *testing.T) {
	f := NewHTTPForwarder("http://example.com/ingest", "test-token")

	if !f.IsConnected() {
		t.Error("IsConnected should always return true for HTTP forwarder")
	}
}

func TestHTTPForwarder_Connect(t *testing.T) {
	f := NewHTTPForwarder("http://example.com/ingest", "test-token")

	err := f.Connect()
	if err != nil {
		t.Errorf("Connect should return nil for HTTP forwarder, got: %v", err)
	}
}

func TestHTTPForwarder_Close(t *testing.T) {
	f := NewHTTPForwarder("http://example.com/ingest", "test-token")
	// Sollte nicht panic-en
	f.Close()
}

func TestHTTPForwarder_Send_InvalidPayload(t *testing.T) {
	f := NewHTTPForwarder("http://example.com/ingest", "test-token")

	// Ungültiges JSON sollte nil zurückgeben (verworfen)
	err := f.Send("zigbee2mqtt/test", []byte("not-json"), time.Now())
	if err != nil {
		t.Errorf("Send with invalid JSON should return nil, got: %v", err)
	}
}
