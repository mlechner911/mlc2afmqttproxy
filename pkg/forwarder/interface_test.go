package forwarder

import (
	"testing"
)

func TestPermanentError(t *testing.T) {
	err := &PermanentError{Err: placeholderError{}}

	if err.Error() != "placeholder" {
		t.Fatalf("unexpected error: %s", err.Error())
	}
}

type placeholderError struct{}

func (placeholderError) Error() string {
	return "placeholder"
}

func TestForwarderInterface(t *testing.T) {
	// HTTPForwarder should implement Forwarder interface
	var fwd Forwarder = NewHTTPForwarder("http://example.com/ingest", "test-token")

	// Test that all interface methods exist and don't panic
	if !fwd.IsConnected() {
		t.Error("IsConnected failed")
	}

	if err := fwd.Connect(); err != nil {
		t.Errorf("Connect failed: %v", err)
	}

	if err := fwd.Subscribe([]string{"test/+"}, func(topic string, payload []byte) {}); err != nil {
		t.Errorf("Subscribe failed: %v", err)
	}

	// SetDownstreamHandler sollte nicht panic-en
	fwd.SetDownstreamHandler(func(topic string, payload []byte) {})

	fwd.Close()
}

func TestDownstreamHandler(t *testing.T) {
	called := false
	h := DownstreamHandler(func(topic string, payload []byte) {
		if topic != "test/topic" {
			t.Errorf("expected test/topic, got: %s", topic)
		}
		if len(payload) != 4 {
			t.Errorf("expected 4 bytes, got: %d", len(payload))
		}
		called = true
	})

	h("test/topic", []byte("test"))

	if !called {
		t.Error("Handler should have been called")
	}
}
