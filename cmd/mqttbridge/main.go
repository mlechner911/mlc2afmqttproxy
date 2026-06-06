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

var Version = "dev"

func main() {
	masterBroker := flag.String("master", "tcp://localhost:1883", "Master MQTT broker URL")
	slaveBroker := flag.String("slave", "tcp://localhost:1884", "Slave MQTT broker URL")
	topic := flag.String("topic", "#", "Topic to subscribe and forward")
	showVersion := flag.Bool("version", false, "Show version and exit")
	showHelp := flag.Bool("help", false, "Show help and exit")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "MQTT Bridge Utility (Version: %s)\n\n", Version)
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "A simple tool to fetch messages from a master MQTT broker and forward them to a slave broker.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
	}

	// We intercept -h or --help via flag package automatically, but explicit --help flag is also added
	flag.Parse()

	if *showHelp {
		flag.Usage()
		os.Exit(0)
	}

	if *showVersion {
		fmt.Printf("mqttbridge version %s\n", Version)
		os.Exit(0)
	}

	log.Printf("Starting MQTT bridge (version %s) from master %s to slave %s on topic %s", Version, *masterBroker, *slaveBroker, *topic)

	// Setup Slave Client
	slaveOpts := mqtt.NewClientOptions().AddBroker(*slaveBroker)
	slaveOpts.SetClientID("mqtt-bridge-slave-" + fmt.Sprint(time.Now().Unix()))
	slaveOpts.SetAutoReconnect(true)

	slaveClient := mqtt.NewClient(slaveOpts)
	if token := slaveClient.Connect(); token.Wait() && token.Error() != nil {
		log.Fatalf("Error connecting to slave broker: %v", token.Error())
	}
	log.Printf("Connected to slave broker: %s", *slaveBroker)
	defer slaveClient.Disconnect(250)

	// Setup Master Client
	masterOpts := mqtt.NewClientOptions().AddBroker(*masterBroker)
	masterOpts.SetClientID("mqtt-bridge-master-" + fmt.Sprint(time.Now().Unix()))
	masterOpts.SetAutoReconnect(true)

	var messageHandler mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
		log.Printf("Forwarding message: Topic: %s", msg.Topic())
		token := slaveClient.Publish(msg.Topic(), msg.Qos(), msg.Retained(), msg.Payload())
		token.Wait()
		if token.Error() != nil {
			log.Printf("Failed to forward message on topic %s: %v", msg.Topic(), token.Error())
		}
	}

	masterOpts.OnConnect = func(c mqtt.Client) {
		log.Printf("Connected to master broker: %s", *masterBroker)
		if token := c.Subscribe(*topic, 0, messageHandler); token.Wait() && token.Error() != nil {
			log.Printf("Error subscribing to master broker topic %s: %v", *topic, token.Error())
		} else {
			log.Printf("Subscribed to master broker topic: %s", *topic)
		}
	}

	masterClient := mqtt.NewClient(masterOpts)
	if token := masterClient.Connect(); token.Wait() && token.Error() != nil {
		log.Fatalf("Error connecting to master broker: %v", token.Error())
	}
	defer masterClient.Disconnect(250)

	// Wait for signals to gracefully shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down MQTT bridge...")
}
