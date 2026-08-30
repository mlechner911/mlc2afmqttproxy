package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_Downstream(t *testing.T) {
	yaml := `
mode: "mqtt"
mqtt:
  upstream: "tcp://cloud.example.com:1883"
  downstream_config:
    subscribe_topics:
      - "cloud/commands/+"
      - "cloud/status/#"
    rewrite:
      match_prefix: "cloud/commands/zigbee2mqtt/"
      replace_with: "zigbee2mqtt/"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg == nil {
		t.Fatal("cfg is nil")
	}

	if cfg.MQTT.Downstream == nil {
		t.Fatal("Downstream should not be nil")
	}

	if len(cfg.MQTT.Downstream.SubscribeTopics) != 2 {
		t.Fatalf("expected 2 topics, got %d", len(cfg.MQTT.Downstream.SubscribeTopics))
	}

	if cfg.MQTT.Downstream.SubscribeTopics[0] != "cloud/commands/+" {
		t.Errorf("expected 'cloud/commands/+ got %s", cfg.MQTT.Downstream.SubscribeTopics[0])
	}

	if cfg.MQTT.Downstream.SubscribeTopics[1] != "cloud/status/#" {
		t.Errorf("expected 'cloud/status/# got %s", cfg.MQTT.Downstream.SubscribeTopics[1])
	}

	if cfg.MQTT.Downstream.Rewrite == nil {
		t.Fatal("Downstream.Rewrite should not be nil")
	}

	if cfg.MQTT.Downstream.Rewrite.MatchPrefix != "cloud/commands/zigbee2mqtt/" {
		t.Errorf("expected match_prefix, got %s", cfg.MQTT.Downstream.Rewrite.MatchPrefix)
	}

	if cfg.MQTT.Downstream.Rewrite.ReplaceWith != "zigbee2mqtt/" {
		t.Errorf("expected replace_with, got %s", cfg.MQTT.Downstream.Rewrite.ReplaceWith)
	}
}

func TestLoadConfig_NoDownstream(t *testing.T) {
	yaml := `
mode: "mqtt"
mqtt:
  upstream: "tcp://cloud.example.com:1883"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.MQTT.Downstream != nil {
		t.Error("Downstream should be nil when not configured")
	}
}

func TestLoadConfig_DownstreamNoRewrite(t *testing.T) {
	yaml := `
mode: "mqtt"
mqtt:
  downstream_config:
    subscribe_topics:
      - "cloud/test/+"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.MQTT.Downstream == nil {
		t.Fatal("Downstream should not be nil")
	}

	if len(cfg.MQTT.Downstream.SubscribeTopics) != 1 {
		t.Fatalf("expected 1 topic, got %d", len(cfg.MQTT.Downstream.SubscribeTopics))
	}

	if cfg.MQTT.Downstream.Rewrite != nil {
		t.Error("Rewrite should be nil when not configured")
	}
}
