// Package config definiert die Datenstrukturen für die Konfiguration des Proxys
// und bietet Funktionen zum Laden derselben aus einer YAML-Datei.
package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Config repräsentiert die Gesamtheit aller Konfigurationsoptionen
// des MLC2AF MQTT Proxys.
type Config struct {
	// Mode steuert den Upstream-Übertragungsmodus. Erlaubte Werte sind "mqtt" oder "http".
	Mode    string      `yaml:"mode"`
	// Storage enthält Konfigurationsoptionen für die BadgerDB-Speicherung.
	Storage StorageConf `yaml:"storage"`
	// MQTT enthält Konfigurationsoptionen für den lokalen und den Upstream-MQTT-Broker.
	MQTT    MQTTConf    `yaml:"mqtt"`
	// HTTP enthält Zugangsdaten und Endpunkte für den HTTP-Upstream.
	HTTP    HTTPConf    `yaml:"http"`
	// Server steuert die Port-Einstellung des Diagnose-Webservers.
	Server  ServerConf  `yaml:"server"`
	// Worker konfiguriert das Verhalten des Store & Forward Hintergrund-Workers.
	Worker  WorkerConf  `yaml:"worker"`
}

// StorageConf enthält Einstellungen für die persistente lokale Datenbank.
type StorageConf struct {
	// Path definiert das Verzeichnis, in dem die BadgerDB-Dateien abgelegt werden.
	Path string `yaml:"path"`
}

// MQTTConf enthält Konfigurationen für die MQTT-Listener sowie die MQTT-Upstream-Verbindung.
type MQTTConf struct {
	// LocalPort ist der lokale Port für den TCP Mochi-MQTT Listener (z.B. 1883).
	LocalPort     int    `yaml:"local_port"`
	// WsPort ist der Port des WebSocket-Listeners für das Live-Dashboard (z.B. 1885).
	WsPort        int    `yaml:"ws_port"`
	// Upstream ist die Adresse des Cloud- oder Master-Brokers (z.B. tcp://cloud.example.com:1883).
	Upstream      string `yaml:"upstream"`
	// Username ist der optionale Benutzername für den Upstream-Broker.
	Username      string `yaml:"username"`
	// Password ist das optionale Passwort für den Upstream-Broker.
	Password      string `yaml:"password"`
	// TimestampMode steuert, wie Zeitstempel übertragen werden ("none", "json_inject", "v5_property").
	TimestampMode  string            `yaml:"timestamp_mode"`
	// TimestampField definiert den JSON-Key beim Modus "json_inject" (Standard: "_ts").
	TimestampField string            `yaml:"timestamp_field"`
	// TopicRewrite erlaubt das Umschreiben von Topics vor dem Senden an den Upstream-Broker.
	TopicRewrite   *TopicRewriteConf `yaml:"topic_rewrite,omitempty"`
}

// TopicRewriteConf ermöglicht das Ersetzen von Topic-Präfixen.
type TopicRewriteConf struct {
	// MatchPrefix sucht nach diesem Präfix (z.B. "zigbee2mqtt/").
	MatchPrefix string `yaml:"match_prefix"`
	// ReplaceWith ersetzt das gefundene Präfix durch diesen Wert (z.B. "cloud/").
	ReplaceWith string `yaml:"replace_with"`
}

// HTTPConf enthält Einstellungen für die HTTP-POST Weiterleitung.
type HTTPConf struct {
	// Endpoint ist der HTTP-Endpunkt der Empfänger-API (MLC Sensor Monitor Ingest).
	Endpoint string `yaml:"endpoint"`
	// Token ist das Authentifizierungstoken, das als "X-Ingest-Token" im HTTP-Header mitgesendet wird.
	Token    string `yaml:"token"`
}

// ServerConf konfiguriert den Diagnose-Webserver.
type ServerConf struct {
	// Port ist der Port, unter dem das Dashboard und die API erreichbar sind (Standard: 8080).
	Port int `yaml:"port"`
	// APIPrefix definiert das Präfix für die Diagnose-REST-API (Standard: /api/v1).
	APIPrefix string `yaml:"api_prefix"`
}

// WorkerConf enthält Einstellungen für den Forward-Worker.
type WorkerConf struct {
	// IntervalMs ist das Standard-Intervall für den Worker-Tick (Standard: 100ms).
	IntervalMs int `yaml:"interval_ms"`
	// MaxBatchSize definiert, wie viele Nachrichten maximal in einer Schleife gesendet werden (Standard: 100).
	// Setzen auf 1 deaktiviert das Batch-Senden und verhält sich wie zuvor.
	MaxBatchSize int `yaml:"max_batch_size"`
	// BatchDelayMs ist die Verzögerung zwischen Nachrichten innerhalb eines Batches in Millisekunden (Standard: 0).
	BatchDelayMs int `yaml:"batch_delay_ms"`
	// RetryMinS ist der minimale Backoff für erneute Verbindungsversuche in Sekunden (Standard: 1s).
	RetryMinS int `yaml:"retry_min_s"`
	// RetryMaxS ist der maximale Backoff für erneute Verbindungsversuche in Sekunden (Standard: 60s).
	RetryMaxS int `yaml:"retry_max_s"`
}

// LoadConfig liest eine YAML-Konfigurationsdatei vom angegebenen Pfad ein
// und wendet Standardwerte für nicht gesetzte Parameter an.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	
	// Default-Werte anwenden, falls nicht gesetzt:
	if cfg.Mode == "" {
		cfg.Mode = "mqtt"
	}
	if cfg.Storage.Path == "" {
		cfg.Storage.Path = "./data"
	}
	if cfg.MQTT.LocalPort == 0 {
		cfg.MQTT.LocalPort = 1883
	}
	if cfg.MQTT.WsPort == 0 {
		cfg.MQTT.WsPort = 1885
	}
	if cfg.MQTT.TimestampMode == "" {
		cfg.MQTT.TimestampMode = "none"
	}
	if cfg.MQTT.TimestampField == "" {
		cfg.MQTT.TimestampField = "_ts"
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Server.APIPrefix == "" {
		cfg.Server.APIPrefix = "/api/v1"
	}
	if cfg.Worker.IntervalMs == 0 {
		cfg.Worker.IntervalMs = 100
	}
	if cfg.Worker.MaxBatchSize == 0 {
		cfg.Worker.MaxBatchSize = 100
	}
	if cfg.Worker.RetryMinS == 0 {
		cfg.Worker.RetryMinS = 1
	}
	if cfg.Worker.RetryMaxS == 0 {
		cfg.Worker.RetryMaxS = 60
	}
	
	return &cfg, nil
}

