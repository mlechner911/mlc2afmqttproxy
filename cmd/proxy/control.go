package main

import (
	"context"
	"log"
	"strings"
	"time"

	mqtt "github.com/mochi-mqtt/server/v2"
	"mlc2afmqttproxy/pkg/broker"
	"mlc2afmqttproxy/pkg/config"
	"mlc2afmqttproxy/pkg/control"
	"mlc2afmqttproxy/pkg/devices"
	"mlc2afmqttproxy/pkg/web"
)

// zigbeeBase ist das Basis-Topic von zigbee2mqtt (mqtt.zigbee_base_topic, Standard zigbee2mqtt).
func zigbeeBase(cfg *config.Config) string {
	if b := strings.TrimSuffix(cfg.MQTT.ZigbeeBaseTopic, "/"); b != "" {
		return b
	}
	return devices.DefaultBaseTopic
}

// controlActive: Mindestens ein Mandant hat freigegebene Geräte.
func controlActive(cfg *config.Config) bool {
	if cfg.Control != nil && len(cfg.Control.Devices) > 0 {
		return true
	}
	for _, r := range cfg.Routes {
		if len(r.Control) > 0 {
			return true
		}
	}
	return false
}

// controlDirect: Befehle gehen in den eigenen Broker (zigbee2mqtt hängt direkt am Proxy).
func controlDirect(cfg *config.Config) bool {
	return cfg.Control == nil || cfg.Control.Broker == ""
}

// prepareControl läuft VOR dem Start des Brokers: Befehle erkennen, im Direktbetrieb den eigenen
// Client des Brokers einschalten.
func prepareControl(cfg *config.Config) {
	broker.SetCommandBase(zigbeeBase(cfg))
	if controlActive(cfg) && controlDirect(cfg) {
		broker.EnableInlineClient()
	}
}

// controlRuntime hält die laufenden Mandanten für die API.
type controlRuntime struct {
	tenants []*control.Tenant
	names   []string
	custs   []string
	journal *control.Journal
	direct  bool
	close   func()
}

// startControl startet den Rückweg je Mandant (Haupt-Upstream und Routen mit control).
func startControl(cfg *config.Config, devs *devices.Tracker, waiters *control.Waiters, local *mqtt.Server) *controlRuntime {
	if !controlActive(cfg) {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	rt := &controlRuntime{journal: control.NewJournal(), direct: controlDirect(cfg)}
	cc := cfg.Control
	if cc == nil {
		cc = &config.ControlConf{}
	}
	zb, closeZB := zigbeePort(ctx, cfg, cc, waiters, local)
	base := zigbeeBase(cfg)
	start := func(name, customer, server, clientID, user, pass string, allow map[string][]string) {
		t, err := control.StartTenant(ctx, control.TenantOpts{
			Name: name, Customer: customer, Base: base, Server: server, ClientID: clientID,
			Username: user, Password: pass, Allow: allow, Lookup: devs.ByIEEE,
			PerMinute: cc.PerMinute, Timeout: time.Duration(cc.TimeoutS) * time.Second,
			Zigbee: zb, Waiters: waiters, Journal: rt.journal,
		})
		if err != nil {
			log.Printf("[control %s] nicht gestartet: %v", name, err)
			return
		}
		rt.tenants, rt.names, rt.custs = append(rt.tenants, t), append(rt.names, name), append(rt.custs, customer)
	}
	if len(cc.Devices) > 0 && cfg.Mode == "mqtt" {
		start("main", cfg.Monitor.CustomerNumber, cfg.MQTT.Upstream, cfg.UpstreamClientID()+"-zb",
			cfg.MQTT.Username, cfg.MQTT.Password, cc.Devices)
	}
	for _, r := range cfg.Routes {
		if len(r.Control) == 0 || r.Mode == "http" {
			continue
		}
		start(r.Name, r.CustomerNumber, r.Upstream, cfg.UpstreamClientID()+"-route-"+r.Name+"-zb",
			r.Username, r.Password, r.Control)
	}
	rt.close = func() {
		for _, t := range rt.tenants {
			t.Close(context.Background())
		}
		closeZB()
		cancel()
	}
	return rt
}

// zigbeePort wählt den Weg zu zigbee2mqtt: eigener Broker (Direktbetrieb) oder control.broker.
func zigbeePort(ctx context.Context, cfg *config.Config, cc *config.ControlConf, w *control.Waiters, local *mqtt.Server) (control.Zigbee, func()) {
	if cc.Broker == "" {
		log.Printf("[control] Befehle über den eigenen Broker (Direktbetrieb)")
		return control.FuncZigbee{
			PublishFn: func(topic string, payload []byte) error { return local.Publish(topic, payload, false, 1) },
			ReadyFn:   func() bool { return broker.LocalClients(local) > 0 },
		}, func() {}
	}
	bz, err := control.DialZigbee(ctx, cc.Broker, cfg.UpstreamClientID()+"-z2m", cc.Username, cc.Password, zigbeeBase(cfg), w)
	if err != nil {
		log.Printf("[control] Broker von zigbee2mqtt %s: %v — Befehle werden abgelehnt", cc.Broker, err)
		return control.FuncZigbee{PublishFn: func(string, []byte) error { return err }, ReadyFn: func() bool { return false }}, func() {}
	}
	log.Printf("[control] Befehle über den Broker von zigbee2mqtt: %s", cc.Broker)
	return bz, func() { bz.Close(context.Background()) }
}

// state liefert den Stand für /api/v1/control (nil = Rückweg aus).
func (rt *controlRuntime) state() *web.ControlResponse {
	if rt == nil {
		return nil
	}
	out := &web.ControlResponse{Enabled: true, Direct: rt.direct, Counts: rt.journal.Counts(), Recent: rt.journal.Recent()}
	for i, t := range rt.tenants {
		out.Tenants = append(out.Tenants, web.ControlTenant{Name: rt.names[i], Customer: rt.custs[i], Allowed: t.Allowed()})
	}
	return out
}
