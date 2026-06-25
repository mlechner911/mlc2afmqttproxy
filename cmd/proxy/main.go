// Package main ist der Haupteinstiegspunkt für den MLC2AF MQTT Proxy.
// Der Proxy fungiert als lokaler MQTT-Broker (Mochi MQTT), puffert eingehende
// Nachrichten in einer BadgerDB und leitet sie per Worker-Goroutine (Store & Forward)
// entweder via HTTP oder Upstream-MQTT weiter. Zusätzlich unterstützt er Downstream-Routing
// (Cloud → lokale Clients) mit Loop-Detection.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mlc2afmqttproxy/pkg/broker"
	"mlc2afmqttproxy/pkg/config"
	"mlc2afmqttproxy/pkg/forwarder"
	"mlc2afmqttproxy/pkg/storage"
	"mlc2afmqttproxy/pkg/svcmon"
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
// 7. Downstream (optional): Nachrichten vom Upstream-Broker an lokale Clients forwarden.
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

	// 3. Upstream-Forwarder initialisieren (VOR dem Mochi-Broker, damit Subscribe() vor Connect() läuft)
	var fwd forwarder.Forwarder
	if cfg.Mode == "http" {
		// HTTP-Modus nutzt die MLC-Sensor-Monitor Ingest-Schnittstelle
		fwd = forwarder.NewHTTPForwarder(cfg.HTTP.Endpoint, cfg.HTTP.Token)
		if cfg.MQTT.Downstream != nil && len(cfg.MQTT.Downstream.SubscribeTopics) > 0 {
			log.Printf("[Downstream] Hinweis: Downstream wird im HTTP-Modus nicht unterstützt")
		}
	} else {
		// MQTT-Modus leitet an einen externen Cloud- oder Master-Broker weiter
		if cfg.MQTT.TimestampMode == "v5_property" {
			fwd = forwarder.NewMQTT5Forwarder(cfg.MQTT.Upstream, cfg.MQTT.Username, cfg.MQTT.Password, cfg.MQTT.TopicAlias, cfg.MQTT.TopicRewrite)
		} else {
			fwd = forwarder.NewMQTTForwarder(cfg.MQTT.Upstream, cfg.MQTT.Username, cfg.MQTT.Password, cfg.MQTT.TimestampMode, cfg.MQTT.TimestampField, cfg.MQTT.TopicRewrite)
		}

		// Downstream: Topics + Rewrite VOR Connect konfigurieren
		if cfg.MQTT.Downstream != nil && len(cfg.MQTT.Downstream.SubscribeTopics) > 0 {
			// Rewrite zuerst setzen (wird im handlePublishReceived gebraucht)
			if cfg.MQTT.Downstream.Rewrite != nil {
				mqttfwd, ok := fwd.(interface{ SetDownRewrite(*config.TopicRewriteConf) })
				if ok {
					mqttfwd.SetDownRewrite(cfg.MQTT.Downstream.Rewrite)
					log.Printf("[Downstream] Topic Rewrite: %s → %s", cfg.MQTT.Downstream.Rewrite.MatchPrefix, cfg.MQTT.Downstream.Rewrite.ReplaceWith)
				}
			}
			// Topics registrieren (Handler kommt später nach Mochi-Setup)
			if err := fwd.Subscribe(cfg.MQTT.Downstream.SubscribeTopics, nil); err != nil {
				log.Printf("[Downstream] Subscribe-Konfiguration fehlgeschlagen: %v", err)
			} else {
				log.Printf("[Downstream] Topics registriert: %d (Handler wird nach Mochi-Setup gesetzt)", len(cfg.MQTT.Downstream.SubscribeTopics))
			}
		}
	}

	// 4. Mochi MQTT Broker starten
	log.Printf("Lokal-Broker lauscht auf TCP :%d und WebSocket :%d", cfg.MQTT.LocalPort, cfg.MQTT.WsPort)
	mochiBroker, err := broker.StartLocalBroker(cfg.MQTT.LocalPort, cfg.MQTT.WsPort, db, cfg.MQTT.Filter, cfg.MQTT.DeduplicateIntervalMs, cfg.MQTT.DeduplicateIgnoreKeys, cfg.Storage.MaxSizeMB)
	if err != nil {
		log.Fatalf("Fehler beim Mochi Broker: %v", err)
	}

	// 5. Forward Worker erzeugen (Start erst nach dem optionalen Pause-Hook, s. u.)
	fwWorker := worker.New(db, fwd, cfg.Worker)
	defer fwWorker.Stop()

	// 6. Diagnose-Webserver und Live-Dashboard auf dem konfigurierten Port starten
	var srv *http.Server
	if cfg.Server.Enable {
		log.Printf("Webserver lauscht auf Port %d mit API-Präfix '%s'", cfg.Server.Port, cfg.Server.APIPrefix)
		srv = web.StartServer(cfg, db, Version, startTime)
	} else {
		log.Println("Diagnose-Webserver ist deaktiviert (server.enable = false).")
	}

	// 7. Downstream-Handler setzen (nach Mochi-Broker-Setup)
	if cfg.MQTT.Downstream != nil && len(cfg.MQTT.Downstream.SubscribeTopics) > 0 && cfg.Mode == "mqtt" {
		downstreamHandler := func(topic string, payload []byte) {
			if err := mochiBroker.Publish(topic, payload, false, 1); err != nil {
				log.Printf("[Downstream] Publish an lokalen Broker fehlgeschlagen: %v", err)
			} else {
				log.Printf("[Downstream] Forwarded an lokale Clients: Topic='%s', %d bytes", topic, len(payload))
			}
		}
		fwd.SetDownstreamHandler(downstreamHandler)
		log.Printf("[Downstream] Handler aktiv — Cloud → lokale Clients")
	}

	// --- Graceful Shutdown Setup ---
	quit := make(chan os.Signal, 1)
	// kill (ohne Parameter) sendet SIGTERM, Strg+C sendet SIGINT
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// 8. Optionale Selbst-Überwachung/-Steuerung über den MLC Sensor Monitor
	// (Heartbeat raus via HTTP-Ingest, Steuerung rein via cmd/svc/<name> auf dem
	// Upstream-Broker). Standardmäßig AN; der Proxy läuft auch ohne Monitor weiter.
	monCtx, monCancel := context.WithCancel(context.Background())
	defer monCancel()
	if cfg.MonitorEnabled() {
		// stop/restart aus der Ferne lösen denselben geordneten Shutdown aus wie ein
		// Signal; ein Supervisor (systemd Restart=always / docker) startet danach neu.
		triggerShutdown := func() {
			select {
			case quit <- syscall.SIGTERM:
			default:
			}
		}
		reporter := svcmon.New(svcmon.Options{
			Device:         cfg.Monitor.Device,
			IngestURL:      cfg.Monitor.IngestURL,
			Token:          cfg.Monitor.Token,
			Interval:       time.Duration(cfg.Monitor.IntervalS) * time.Second,
			ControlEnabled: cfg.MonitorControlEnabled(),
			Broker:         cfg.MQTT.Upstream,
			Username:       cfg.MQTT.Username,
			Password:       cfg.MQTT.Password,
			Version:        Version,
			OnStop:         triggerShutdown,
			OnRestart:      triggerShutdown,
		})
		// „pause"/„drain" lassen den Upstream-Versand ruhen (Daten bleiben gepuffert).
		fwWorker.SetPauseCheck(reporter.Paused)
		reporter.Start(monCtx)
	} else {
		log.Println("[svcmon] Monitor-Anbindung deaktiviert (monitor.enabled = false).")
	}

	// 9. Forward-Worker jetzt starten (Pause-Hook ist gesetzt).
	fwWorker.Start()

	sig := <-quit
	log.Printf("Signal %v empfangen. Beende Proxy geordnet (Graceful Shutdown)...", sig)

	if srv != nil {
		// Context mit Timeout für den Webserver-Shutdown (max 5 Sekunden)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("Webserver Shutdown fehlerhaft: %v", err)
		}
		log.Println("Webserver gestoppt.")
	}

	log.Println("Stoppe Mochi MQTT Broker...")
	_ = mochiBroker.Close()

	log.Println("Datenbank und Worker werden sauber geschlossen...")
	// defer db.Close() und defer fwWorker.Stop() werden nun beim Verlassen der main() sauber ausgeführt!
}
