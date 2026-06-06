package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Mode    string      `yaml:"mode"` // "mqtt" or "http"
	Storage StorageConf `yaml:"storage"`
	MQTT    MQTTConf    `yaml:"mqtt"`
	HTTP    HTTPConf    `yaml:"http"`
	Server  ServerConf  `yaml:"server"`
}

type StorageConf struct {
	Path string `yaml:"path"`
}

type MQTTConf struct {
	LocalPort int    `yaml:"local_port"`
	WsPort    int    `yaml:"ws_port"`
	Upstream  string `yaml:"upstream"`
	Username  string `yaml:"username"`
	Password  string `yaml:"password"`
}

type HTTPConf struct {
	Endpoint string `yaml:"endpoint"`
	Token    string `yaml:"token"`
}

type ServerConf struct {
	Port int `yaml:"port"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	
	// Apply defaults
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
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	
	return &cfg, nil
}
