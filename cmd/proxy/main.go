package main

import (
	"log"

	"mlc2afmqttproxy/pkg/broker"
	"mlc2afmqttproxy/pkg/config"
	"mlc2afmqttproxy/pkg/forwarder"
	"mlc2afmqttproxy/pkg/storage"
	"mlc2afmqttproxy/pkg/web"
	"mlc2afmqttproxy/pkg/worker"
)

var Version = "dev"

func main() {
	log.Printf("Starte MLC2AF MQTT Proxy (Version: %s)...", Version)

	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		log.Fatalf("Fehler beim Laden der Konfiguration: %v", err)
	}

	// Init BadgerDB
	db, err := storage.InitBadger(cfg.Storage.Path)
	if err != nil {
		log.Fatalf("Fehler bei BadgerDB: %v", err)
	}
	defer db.Close()

	// Init Forwarder (Upstream)
	var fwd forwarder.Forwarder
	if cfg.Mode == "http" {
		fwd = forwarder.NewHTTPForwarder(cfg.HTTP.Endpoint, cfg.HTTP.Token)
	} else {
		if cfg.MQTT.TimestampMode == "v5_property" {
			fwd = forwarder.NewMQTT5Forwarder(cfg.MQTT.Upstream, cfg.MQTT.Username, cfg.MQTT.Password)
		} else {
			fwd = forwarder.NewMQTTForwarder(cfg.MQTT.Upstream, cfg.MQTT.Username, cfg.MQTT.Password, cfg.MQTT.TimestampMode)
		}
	}

	// Init Mochi Broker
	_, err = broker.StartLocalBroker(cfg.MQTT.LocalPort, cfg.MQTT.WsPort, db)
	if err != nil {
		log.Fatalf("Fehler beim Mochi Broker: %v", err)
	}

	// Starte Forward Worker
	fwWorker := worker.New(db, fwd)
	fwWorker.Start()
	defer fwWorker.Stop()

	// Start Web Server
	log.Printf("Webserver lauscht auf Port %d", cfg.Server.Port)
	if err := web.StartServer(cfg.Server.Port, db, Version); err != nil {
		log.Fatalf("Fehler beim Webserver: %v", err)
	}
}
