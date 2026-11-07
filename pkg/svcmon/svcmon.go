// Package svcmon bindet den Edge-Proxy OPTIONAL (standardmäßig aktiv) an den
// MLC Sensor Monitor an — als „Dienst" mit Heartbeat + Fernsteuerung, ohne den
// Proxy fest an die Monitor-Software zu koppeln.
//
//   - Heartbeat (raus): periodischer HTTP-POST an den Ingest-Endpunkt des Monitors
//     (`service_up`/`service_state`/`svc_uptime_s`). Der Monitor legt daraus
//     automatisch das Gerät `svc-edgeproxy` an; bleibt der Heartbeat aus, feuert der
//     Monitor selbst ein Ereignis (NO_DATA) — der Proxy ist damit mitüberwacht.
//   - Steuerung (rein, optional): abonniert auf dem (ohnehin genutzten) Upstream-
//     MQTT-Broker das Topic `cmd/svc/<name>` und beantwortet `pause/resume/drain/
//     restart/stop`, mit Ack auf `cmd/svc/<name>/ack`.
//
// Bewusst KEIN Import interner Monitor-Pakete — nur HTTP + paho-MQTT. Ohne
// erreichbaren Monitor läuft der Proxy normal weiter (Heartbeat/Steuerung
// scheitern still und werden wiederholt).
package svcmon

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// Dienst-Zustände (Spalte service_state im Monitor): 1 läuft · 2 pausiert · 3 leert.
const (
	StateRunning  int32 = 1
	StatePaused   int32 = 2
	StateDraining int32 = 3
)

// Options bündelt die Laufzeit-Parameter des Reporters.
type Options struct {
	Device         string        // Gerätename im Monitor, z. B. "svc-edgeproxy"
	IngestURL      string        // HTTP-Ingest-Endpunkt (Heartbeat). Leer ⇒ Heartbeat aus.
	Token          string        // X-Ingest-Token. Leer ⇒ Heartbeat aus.
	Interval       time.Duration // Heartbeat-Intervall (Default 30 s)
	ControlEnabled bool          // Fernsteuerung via cmd/svc/<name>
	Broker         string        // Upstream-MQTT (tcp://host:port) für die Steuerung
	Username       string        // Upstream-MQTT-Benutzer (= Proxy-Upstream-Creds)
	Password       string        // Upstream-MQTT-Passwort / Token
	Version        string        // Proxy-Version (nur fürs Log)
	OnStop         func()        // „stop"  → Proxy geordnet beenden
	OnRestart      func()        // „restart" → Proxy beenden (Supervisor startet neu)
}

// Reporter hält Heartbeat- und Steuer-Verbindung.
type Reporter struct {
	o      Options
	name   string // logischer Name (Device ohne "svc-") → cmd/svc/<name>
	state  atomic.Int32
	start  time.Time
	httpc  *http.Client
	client mqtt.Client
}

// New erzeugt einen Reporter (Zustand „läuft").
func New(o Options) *Reporter {
	if o.Interval <= 0 {
		o.Interval = 30 * time.Second
	}
	r := &Reporter{
		o:     o,
		name:  strings.TrimPrefix(o.Device, "svc-"),
		start: time.Now(),
		httpc: &http.Client{Timeout: 10 * time.Second},
	}
	r.state.Store(StateRunning)
	return r
}

// Paused meldet, ob die Weiterleitung aktuell ruhen soll (pausiert ODER leert) —
// der Worker konsultiert das, um das Upstream-Senden zu pausieren (Daten bleiben
// derweil im Store&Forward-Puffer, kein Verlust).
func (r *Reporter) Paused() bool {
	s := r.state.Load()
	return s == StatePaused || s == StateDraining
}

// Start fährt Heartbeat (und optional die Steuerung) hoch; läuft bis ctx endet.
func (r *Reporter) Start(ctx context.Context) {
	if r.o.ControlEnabled && r.o.Broker != "" {
		r.connectControl(ctx)
	}
	if r.o.IngestURL != "" && r.o.Token != "" {
		go r.heartbeatLoop(ctx)
		log.Printf("[svcmon] Heartbeat aktiv → %s als '%s' (alle %s)", r.o.IngestURL, r.o.Device, r.o.Interval)
	} else {
		log.Printf("[svcmon] Heartbeat aus (kein ingest_url/token) — Proxy läuft normal weiter")
	}
}

func (r *Reporter) heartbeatLoop(ctx context.Context) {
	r.sendHeartbeat() // sofort melden → Gerät früh anlegen
	t := time.NewTicker(r.o.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.sendHeartbeat()
		}
	}
}

func (r *Reporter) sendHeartbeat() {
	body, _ := json.Marshal(map[string]any{
		"device": r.o.Device,
		"readings": []map[string]any{
			{"key": "service_up", "value": 1},
			{"key": "service_state", "value": r.state.Load()},
			{"key": "svc_uptime_s", "value": int(time.Since(r.start).Seconds())},
		},
	})
	req, err := http.NewRequest(http.MethodPost, r.o.IngestURL, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ingest-Token", r.o.Token)
	resp, err := r.httpc.Do(req)
	if err != nil {
		log.Printf("[svcmon] Heartbeat fehlgeschlagen: %v", err)
		return
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 300 {
		log.Printf("[svcmon] Heartbeat abgelehnt: HTTP %d", resp.StatusCode)
	}
}

// connectControl verbindet (best-effort, Auto-Reconnect) mit dem Upstream-Broker
// und abonniert cmd/svc/<name>. Scheitert die Verbindung, läuft der Proxy normal
// weiter; paho versucht im Hintergrund weiter zu verbinden.
func (r *Reporter) connectControl(ctx context.Context) {
	cmdTopic := "cmd/svc/" + r.name
	opts := mqtt.NewClientOptions().
		AddBroker(r.o.Broker).
		SetClientID(r.o.Device + "-ctl").
		SetUsername(r.o.Username).
		SetPassword(r.o.Password).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectTimeout(10 * time.Second).
		SetOnConnectHandler(func(c mqtt.Client) {
			if t := c.Subscribe(cmdTopic, 1, r.onCommand); t.WaitTimeout(10*time.Second) && t.Error() != nil {
				log.Printf("[svcmon] Subscribe %s fehlgeschlagen: %v", cmdTopic, t.Error())
				return
			}
			log.Printf("[svcmon] Fernsteuerung aktiv: %s", cmdTopic)
		})
	r.client = mqtt.NewClient(opts)
	if t := r.client.Connect(); t.WaitTimeout(10*time.Second) && t.Error() != nil {
		log.Printf("[svcmon] Control-Verbindung (vorerst) fehlgeschlagen: %v — Reconnect läuft", t.Error())
	}
	go func() { <-ctx.Done(); r.client.Disconnect(250) }()
}

// onCommand führt die Aktion aus und ackt auf cmd/svc/<name>/ack (Format wie der
// serviceagent des Monitors: service/action/command_id/status/error/state).
func (r *Reporter) onCommand(_ mqtt.Client, m mqtt.Message) {
	var cmd struct {
		Action    string `json:"action"`
		CommandID string `json:"command_id"`
	}
	_ = json.Unmarshal(m.Payload(), &cmd)
	action := strings.TrimSpace(cmd.Action)
	if action == "" {
		return
	}

	status, errMsg := "ok", ""
	var post func()
	switch action {
	case "pause":
		r.state.Store(StatePaused)
	case "resume":
		r.state.Store(StateRunning)
	case "drain":
		r.state.Store(StateDraining)
	case "stop":
		post = r.o.OnStop
	case "restart":
		post = r.o.OnRestart
	default:
		status = "unknown"
	}

	r.ack(action, cmd.CommandID, status, errMsg)
	log.Printf("[svcmon] Befehl '%s' → %s", action, status)

	// stop/restart erst NACH dem Ack auslösen (sonst beendet sich der Prozess, bevor
	// das Ack rausging). Kurze Verzögerung, damit das Ack sicher gesendet ist.
	if post != nil {
		go func() {
			time.Sleep(500 * time.Millisecond)
			post()
		}()
	}
}

func (r *Reporter) ack(action, commandID, status, errMsg string) {
	if r.client == nil {
		return
	}
	ack, _ := json.Marshal(map[string]any{
		"service": r.name, "action": action, "command_id": commandID,
		"status": status, "error": errMsg, "state": r.state.Load(),
	})
	r.client.Publish("cmd/svc/"+r.name+"/ack", 1, false, ack)
}
