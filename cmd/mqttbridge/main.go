// Package main implementiert ein CLI-Werkzeug zur Überbrückung zweier MQTT-Broker.
// Das Tool abonniert ein konfiguriertes Topic auf einem Master-Broker und leitet
// alle eingehenden Nachrichten 1:1 an einen Slave-Broker weiter (inklusive QoS und Payload).
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
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

	log.Printf("Starte MQTT Bridge (Version %s) von Master %s zu Slave %s auf Topic '%s'", Version, *masterBroker, *slaveBroker, *topic)

	// 1. Slave-Client konfigurieren (Ziel-Broker)
	slaveOpts := mqtt.NewClientOptions().AddBroker(*slaveBroker)
	slaveOpts.SetClientID("mqtt-bridge-slave-" + fmt.Sprint(time.Now().Unix()))
	slaveOpts.SetAutoReconnect(true)

	slaveClient := mqtt.NewClient(slaveOpts)
	if token := slaveClient.Connect(); token.Wait() && token.Error() != nil {
		log.Fatalf("Fehler beim Verbinden mit dem Slave-Broker: %v", token.Error())
	}
	log.Printf("Verbunden mit Slave-Broker: %s", *slaveBroker)
	defer slaveClient.Disconnect(250)

	// 2. Master-Client konfigurieren (Quell-Broker)
	masterOpts := mqtt.NewClientOptions().AddBroker(*masterBroker)
	masterOpts.SetClientID("mqtt-bridge-master-" + fmt.Sprint(time.Now().Unix()))
	masterOpts.SetAutoReconnect(true)

	// Message-Handler für eingehende Nachrichten vom Master-Broker.
	// Jede empfangene Nachricht wird an den Slave-Broker weitergeleitet.
	var messageHandler mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
		log.Printf("Leite Nachricht weiter: Topic='%s', QoS=%d, Retained=%t, Länge=%d Bytes", msg.Topic(), msg.Qos(), msg.Retained(), len(msg.Payload()))
		token := slaveClient.Publish(msg.Topic(), msg.Qos(), msg.Retained(), msg.Payload())
		token.Wait()
		if token.Error() != nil {
			log.Printf("Fehler beim Weiterleiten an den Slave-Broker (Topic '%s'): %v", msg.Topic(), token.Error())
		}
	}

	// Sobald die Verbindung steht, wird das Topic abonniert.
	masterOpts.OnConnect = func(c mqtt.Client) {
		log.Printf("Verbunden mit Master-Broker: %s", *masterBroker)
		if token := c.Subscribe(*topic, 0, messageHandler); token.Wait() && token.Error() != nil {
			log.Printf("Fehler beim Abonnieren des Topics '%s' auf dem Master-Broker: %v", *topic, token.Error())
		} else {
			log.Printf("Topic erfolgreich abonniert: %s", *topic)
		}
	}

	masterClient := mqtt.NewClient(masterOpts)
	if token := masterClient.Connect(); token.Wait() && token.Error() != nil {
		log.Fatalf("Fehler beim Verbinden mit dem Master-Broker: %v", token.Error())
	}
	defer masterClient.Disconnect(250)

	// 3. Graceful Shutdown einrichten
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("Beende MQTT Bridge...")
}

