// Package main ist der Haupteinstiegspunkt für den MLC2AF MQTT Proxy.
// Der Proxy fungiert als lokaler MQTT-Broker (Mochi MQTT), puffert eingehende
// Nachrichten in einer BadgerDB und leitet sie per Worker-Goroutine (Store & Forward)
// entweder via HTTP oder Upstream-MQTT weiter.
package main

import (
	"flag"
	"log"
	"time"

	"mlc2afmqttproxy/pkg/broker"
	"mlc2afmqttproxy/pkg/config"
	"mlc2afmqttproxy/pkg/forwarder"
	"mlc2afmqttproxy/pkg/storage"
	"mlc2afmqttproxy/pkg/web"
	"mlc2afmqttproxy/pkg/worker"
)

// Version wird zur Build-Zeit über ldflags gesetzt (z.B. -ldflags "-X main.Version=v1.0.0").
// Der Standardwert ist "dev".
var Version = "dev"

// main initialisiert und startet alle Komponenten des Proxys.
// Ablauf:
// 1. Laden der Konfiguration (config.yaml).
// 2. Initialisierung der persistenten BadgerDB für das Store & Forward Buffering.
// 3. Konfiguration des Upstream-Forwarders (entweder HTTP Ingest-API oder Upstream-MQTT v3/v5).
// 4. Starten des lokalen Mochi-MQTT-Brokers (inkl. WebSocket-Listener für das Live-Dashboard).
// 5. Starten der Worker-Goroutine zum sequentiellen Abarbeiten des Puffers.
// 6. Starten des Gin-Webservers zur Auslieferung des Svelte-Dashboards und der Status-API.
func main() {
	configPath := flag.String("config", "config.yaml", "Pfad zur Konfigurationsdatei")
	flag.Parse()

	startTime := time.Now()
	log.Printf("Starte MLC Edge Proxy (Version: %s)...", Version)

	// 1. Konfiguration laden
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Fehler beim Laden der Konfiguration: %v", err)
	}

	// 2. BadgerDB initialisieren
	db, err := storage.InitBadger(cfg.Storage.Path)
	if err != nil {
		log.Fatalf("Fehler bei BadgerDB: %v", err)
	}
	defer db.Close()

	// 3. Upstream-Forwarder initialisieren
	var fwd forwarder.Forwarder
	if cfg.Mode == "http" {
		// HTTP-Modus nutzt die MLC-Sensor-Monitor Ingest-Schnittstelle
		fwd = forwarder.NewHTTPForwarder(cfg.HTTP.Endpoint, cfg.HTTP.Token)
	} else {
		// MQTT-Modus leitet an einen externen Cloud- oder Master-Broker weiter
		if cfg.MQTT.TimestampMode == "v5_property" {
			fwd = forwarder.NewMQTT5Forwarder(cfg.MQTT.Upstream, cfg.MQTT.Username, cfg.MQTT.Password, cfg.MQTT.TopicRewrite)
		} else {
			fwd = forwarder.NewMQTTForwarder(cfg.MQTT.Upstream, cfg.MQTT.Username, cfg.MQTT.Password, cfg.MQTT.TimestampMode, cfg.MQTT.TimestampField, cfg.MQTT.TopicRewrite)
		}
	}

	// 4. Mochi MQTT Broker starten
	_, err = broker.StartLocalBroker(cfg.MQTT.LocalPort, cfg.MQTT.WsPort, db, cfg.MQTT.Filter)
	if err != nil {
		log.Fatalf("Fehler beim Mochi Broker: %v", err)
	}

	// 5. Forward Worker im Hintergrund starten
	fwWorker := worker.New(db, fwd, cfg.Worker)
	fwWorker.Start()
	defer fwWorker.Stop()

	// 6. Diagnose-Webserver und Live-Dashboard auf dem konfigurierten Port starten
	log.Printf("Webserver lauscht auf Port %d mit API-Präfix '%s'", cfg.Server.Port, cfg.Server.APIPrefix)
	if err := web.StartServer(cfg, db, Version, startTime); err != nil {
		log.Fatalf("Fehler beim Webserver: %v", err)
	}
}

