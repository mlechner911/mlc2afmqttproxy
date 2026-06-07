// Package main implementiert ein CLI-Werkzeug zur Überbrückung zweier MQTT-Broker.
// Das Tool abonniert ein konfiguriertes Topic auf einem Master-Broker und leitet
// alle eingehenden Nachrichten 1:1 an einen Slave-Broker weiter (inklusive QoS und Payload).
package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// Version wird zur Build-Zeit via ldflags injiziert (z.B. -ldflags "-X main.Version=v1.0.0").
// Standardwert ist "dev".
var Version = "dev"

// main ist der Haupteinstiegspunkt für das mqttbridge CLI-Tool.
// Ablauf:
// 1. Definition und Parsen der Kommandozeilenparameter (Master, Slave, Topic, Version, Help).
// 2. Verbindungsaufbau zum Slave-Broker (Publish-Ziel).
// 3. Verbindungsaufbau zum Master-Broker (Subscribe-Quelle) mit Registrierung des MessageHandlers.
// 4. Weiterleitung eingehender Nachrichten.
// 5. Warten auf Interrupt- oder Abbruch-Signale zur sauberen Trennung der Verbindungen.
func main() {
	// CLI Flags definieren
	masterBroker := flag.String("master", "tcp://localhost:1883", "Master-MQTT-Broker URL (z.B. tcp://192.168.1.50:1883)")
	slaveBroker := flag.String("slave", "tcp://localhost:1884", "Slave-MQTT-Broker URL (z.B. tcp://localhost:1883)")
	topic := flag.String("topic", "#", "MQTT-Topic, das abonniert und weitergeleitet werden soll (Wildcards erlaubt)")
	bidi := flag.Bool("bidi", false, "Aktiviert die bidirektionale Weiterleitung (Slave -> Master) inkl. Loop-Detection")
	showVersion := flag.Bool("version", false, "Zeigt die Programmversion an und beendet das Tool")
	showHelp := flag.Bool("help", false, "Zeigt diese Hilfe an und beendet das Tool")

	// Eigene Usage-Hilfe bei Fehlern oder --help definieren
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "MQTT Bridge Utility (Version: %s)\n\n", Version)
		fmt.Fprintf(os.Stderr, "Nutzung: %s [Optionen]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Dieses Hilfsprogramm abonniert ein Topic auf einem Master-Broker und leitet alle\n")
		fmt.Fprintf(os.Stderr, "eingehenden Nachrichten an einen Slave-Broker weiter.\n\n")
		fmt.Fprintf(os.Stderr, "Optionen:\n")
		flag.PrintDefaults()
	}

	// Flags parsen
	flag.Parse()

	// Hilfe-Flag auswerten
	if *showHelp {
		flag.Usage()
		os.Exit(0)
	}

	// Versions-Flag auswerten
	if *showVersion {
		fmt.Printf("mqttbridge version %s\n", Version)
		os.Exit(0)
	}

	bidiText := "Unidirektional"
	if *bidi {
		bidiText = "Bidirektional"
	}
	log.Printf("Starte MQTT Bridge (Version %s) | %s | Master: %s <-> Slave: %s | Topic: '%s'", Version, bidiText, *masterBroker, *slaveBroker, *topic)

	// Loop-Detection Cache, um "Ping-Pong" Endlosschleifen bei Bidirektionalität zu vermeiden
	var lastSent sync.Map

	// Forwarder Funktion (Richtung flexibel)
	forwardMsg := func(direction string, srcClient, dstClient mqtt.Client, msg mqtt.Message) {
		topic := msg.Topic()
		payload := msg.Payload()

		// Loop-Detection: Prüfen, ob wir genau diese Nachricht gerade in die ANDERE Richtung gesendet haben
		otherDir := "m2s"
		if direction == "m2s" {
			otherDir = "s2m"
		}

		if val, ok := lastSent.Load(otherDir + "_" + topic); ok {
			if bytes.Equal(val.([]byte), payload) {
				// Echo erkannt! Wir löschen es aus dem Cache und ignorieren die Nachricht.
				lastSent.Delete(otherDir + "_" + topic)
				return
			}
		}

		log.Printf("[%s] Leite Nachricht weiter: Topic='%s', QoS=%d, Retained=%t, Länge=%d Bytes", direction, topic, msg.Qos(), msg.Retained(), len(payload))
		
		// Wir merken uns, dass wir diese Nachricht in unsere Richtung senden
		lastSent.Store(direction+"_"+topic, payload)

		token := dstClient.Publish(topic, msg.Qos(), msg.Retained(), payload)
		token.Wait()
		if token.Error() != nil {
			log.Printf("[%s] Fehler beim Publish (Topic '%s'): %v", direction, topic, token.Error())
		}
	}

	// 1. Slave-Client konfigurieren (Ziel-Broker)
	slaveOpts := mqtt.NewClientOptions().AddBroker(*slaveBroker)
	slaveOpts.SetClientID("mqtt-bridge-slave-" + fmt.Sprint(time.Now().Unix()))
	slaveOpts.SetAutoReconnect(true)

	// Optional: OnConnect für Slave, falls bidi aktiv ist
	slaveOpts.OnConnect = func(c mqtt.Client) {
		log.Printf("Verbunden mit Slave-Broker: %s", *slaveBroker)
		if *bidi {
			// Hier ist c == slaveClient und wir leiten an masterClient (wird gleich definiert) weiter.
			// Da masterClient zu diesem Zeitpunkt evtl. noch nicht da ist, holen wir ihn global oder verzögern.
			// Wir übergeben dstClient dynamisch im Handler.
		}
	}

	slaveClient := mqtt.NewClient(slaveOpts)
	if token := slaveClient.Connect(); token.Wait() && token.Error() != nil {
		log.Fatalf("Fehler beim Verbinden mit dem Slave-Broker: %v", token.Error())
	}
	defer slaveClient.Disconnect(250)

	// 2. Master-Client konfigurieren (Quell-Broker)
	masterOpts := mqtt.NewClientOptions().AddBroker(*masterBroker)
	masterOpts.SetClientID("mqtt-bridge-master-" + fmt.Sprint(time.Now().Unix()))
	masterOpts.SetAutoReconnect(true)

	// Message-Handler für eingehende Nachrichten vom Master-Broker (Master -> Slave)
	var masterToSlaveHandler mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
		forwardMsg("m2s", client, slaveClient, msg)
	}

	masterOpts.OnConnect = func(c mqtt.Client) {
		log.Printf("Verbunden mit Master-Broker: %s", *masterBroker)
		if token := c.Subscribe(*topic, 0, masterToSlaveHandler); token.Wait() && token.Error() != nil {
			log.Printf("Fehler beim Abonnieren des Topics '%s' auf dem Master-Broker: %v", *topic, token.Error())
		} else {
			log.Printf("Topic (Master -> Slave) erfolgreich abonniert: %s", *topic)
		}
	}

	masterClient := mqtt.NewClient(masterOpts)
	if token := masterClient.Connect(); token.Wait() && token.Error() != nil {
		log.Fatalf("Fehler beim Verbinden mit dem Master-Broker: %v", token.Error())
	}
	defer masterClient.Disconnect(250)

	// Falls Bidirektionalität aktiv ist, müssen wir jetzt, da masterClient existiert, 
	// das Subscribe auf dem Slave-Client einrichten (Slave -> Master).
	if *bidi {
		var slaveToMasterHandler mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
			forwardMsg("s2m", client, masterClient, msg)
		}
		if token := slaveClient.Subscribe(*topic, 0, slaveToMasterHandler); token.Wait() && token.Error() != nil {
			log.Printf("Fehler beim Abonnieren des Topics '%s' auf dem Slave-Broker: %v", *topic, token.Error())
		} else {
			log.Printf("Topic (Slave -> Master) erfolgreich abonniert: %s", *topic)
		}
	}

	// 3. Graceful Shutdown einrichten
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("Beende MQTT Bridge...")
}

