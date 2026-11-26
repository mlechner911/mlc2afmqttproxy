// Package config definiert die Datenstrukturen für die Konfiguration des Proxys
// und bietet Funktionen zum Laden derselben aus einer YAML-Datei.
package config

import (
	"fmt"
	"log"
	"net"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config repräsentiert die Gesamtheit aller Konfigurationsoptionen
// des MLC2AF MQTT Proxys.
type Config struct {
	// Mode steuert den Upstream-Übertragungsmodus. Erlaubte Werte sind "mqtt" oder "http".
	Mode    string       `yaml:"mode"`
	// Storage enthält Konfigurationsoptionen für die BadgerDB-Speicherung.
	Storage StorageConf  `yaml:"storage"`
	// MQTTConf enthält Konfigurationen für den lokalen und den Upstream-MQTT-Broker.
	MQTT    MQTTConf     `yaml:"mqtt"`
	// HTTP enthält Zugangsdaten und Endpunkte für den HTTP-Upstream.
	HTTP    HTTPConf     `yaml:"http"`
	// Server steuert die Port-Einstellung des Diagnose-Webservers.
	Server  ServerConf   `yaml:"server"`
	// Worker konfiguriert das Verhalten des Store & Forward Hintergrund-Workers.
	Worker  WorkerConf   `yaml:"worker"`
	// Monitor steuert die OPTIONALE Selbst-Überwachung/-Steuerung über den MLC
	// Sensor Monitor (Heartbeat + Fernsteuerung). Per Default AKTIV.
	Monitor MonitorConf  `yaml:"monitor"`
	// Routes sind ZUSÄTZLICHE Upstreams neben dem Haupt-Upstream (mode/mqtt/http oben):
	// jede mit eigenem Ziel und Zugang (z. B. Ingest-Token eines anderen Mandanten), eigenem
	// Filter, optional eigener Geräteliste — und eigenem Puffer + Worker, damit ein
	// unerreichbares Ziel die anderen nicht aufhält. Leer = Verhalten wie bisher.
	Routes []RouteConf `yaml:"routes,omitempty"`

	// Control ist der Rückweg: freigegebene Befehle vom Monitor an zigbee2mqtt (siehe ControlConf).
	Control *ControlConf `yaml:"control,omitempty"`
}

// ControlEnabled: Der Rückweg ist an, solange control.enabled nicht false ist.
func (c *Config) ControlEnabled() bool {
	return c.Control == nil || c.Control.Enabled == nil || *c.Control.Enabled
}

// ControlConf: Geräte aus dem MLC Sensor Monitor schalten (seit 1.5). Standardmäßig AN, ohne
// Konfiguration (allow: operate): Der Monitor darf an jedem Gerät, das zigbee2mqtt kennt, die
// Bedienfelder setzen (state, brightness, color_temp, color, position, state_<kanal>) — sofern das
// Gerät sie als beschreibbar meldet. Welche Geräte tatsächlich geschaltet werden, entscheidet der
// Monitor. Der Proxy schaltet nie selbst: Er reicht Befehle von …/<ieee>/set (Abo Subscribe,
// Vorgabe cmd/zb/#) an <base>/<gerät>/set weiter und quittiert auf demselben Topic mit /ack.
type ControlConf struct {
	// Enabled: false schaltet den Rückweg ganz ab (auch für alle Routen). Fehlt = an.
	Enabled *bool `yaml:"enabled,omitempty"`
	// Allow: operate (Bedienfelder) | writable (alles, was das Gerät als setzbar meldet, auch
	// Einstellungen/Kalibrierung) | list (nur Devices). Fehlt es: list, wenn Devices gesetzt ist
	// (fail closed), sonst operate. Gilt für den Haupt-Upstream.
	Allow string `yaml:"allow,omitempty"`
	// Devices: Felder je Gerät (IEEE → Felder). Bei list die einzige Freigabe; bei ausdrücklich
	// operate/writable nur ZUSÄTZLICHE Felder. Nur für den Haupt-Upstream; Routen haben routes[].control.
	Devices map[string][]string `yaml:"devices,omitempty"`
	// Subscribe: Topic-Filter für Befehle am Haupt-Upstream (Standard "cmd/zb/#"). Welche Befehle
	// davon ankommen, entscheidet der Upstream (mlcmonit: nur der eigene Mandant).
	Subscribe string `yaml:"subscribe,omitempty"`
	// Broker, an dem zigbee2mqtt hängt. Leer = der eigene Broker (Direktbetrieb); sonst z. B.
	// "tcp://127.0.0.1:1883" (zigbee2mqtt an Mosquitto, Proxy über die mqttbridge).
	Broker   string `yaml:"broker,omitempty"`
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
	// TimeoutS: so lange wartet die Quittung auf die Zustandsmeldung des Geräts (Standard 10).
	TimeoutS int `yaml:"timeout_s,omitempty"`
	// PerMinute: höchstens so viele Befehle je Gerät und Minute (Standard 10).
	PerMinute int `yaml:"per_minute,omitempty"`
}

// RouteConf beschreibt einen zusätzlichen Upstream (siehe Config.Routes).
type RouteConf struct {
	// Name identifiziert die Route (Puffer-Verzeichnis <storage.path>/routes/<name>, Logs).
	Name string `yaml:"name"`
	// Mode "mqtt" (Default) oder "http".
	Mode string `yaml:"mode"`
	// MQTT-Ziel und Zugang (Mode mqtt). Zeitstempel-Modus wie beim Haupt-Upstream.
	Upstream   string `yaml:"upstream"`
	Username   string `yaml:"username"`
	Password   string `yaml:"password"`
	TopicAlias bool   `yaml:"topic_alias"`
	// HTTP-Ziel (Mode http).
	HTTP HTTPConf `yaml:"http"`
	// Filter wirkt ZUSÄTZLICH zum globalen Filter (mqtt.filter).
	Filter *FilterConf `yaml:"filter,omitempty"`
	// Devices: nur diese Zigbee-Geräte (friendly_name oder IEEE-Adresse). Dann gehen nur
	// <base>/<gerät>[/…] und ein auf diese Geräte GEKÜRZTES <base>/bridge/devices hinaus —
	// sonst würde das Ziel alle Geräte des Standorts anlegen. Leer = alle (nach Filter).
	Devices []string `yaml:"devices,omitempty"`
	// BaseTopic von Zigbee2MQTT (Default "zigbee2mqtt").
	BaseTopic string `yaml:"base_topic"`
	// CustomerNumber wird nicht mehr gebraucht (seit 1.5 abonniert der Rückweg control_subscribe);
	// ein vorhandener Wert ist erlaubt und bleibt ungenutzt.
	CustomerNumber string `yaml:"customer_number,omitempty"`
	// ControlSubscribe: Topic-Filter für Befehle über diese Route (Standard "cmd/zb/#").
	ControlSubscribe string `yaml:"control_subscribe,omitempty"`
	// ControlAllow: wie control.allow, für diese Route (fehlt: list, wenn Control gesetzt, sonst operate).
	ControlAllow string `yaml:"control_allow,omitempty"`
	// Control: Felder je Gerät dieser Route (IEEE → Felder), Bedeutung wie control.devices.
	// Geschaltet werden über eine Route nur ihre Devices (leer = alle).
	Control map[string][]string `yaml:"control,omitempty"`
}

// MonitorConf konfiguriert die optionale Anbindung an den MLC Sensor Monitor:
// ein periodischer Heartbeat (der Proxy erscheint dort als Dienst-Kachel und ein
// Ausfall löst automatisch ein Ereignis aus) sowie eine MQTT-Fernsteuerung
// (pause/resume/drain/restart/stop). Beides ist additiv und **standardmäßig an** —
// der Proxy läuft aber auch ohne (z. B. wenn kein Monitor erreichbar ist). Es werden
// KEINE internen Pakete des Monitors importiert (lose Kopplung).
type MonitorConf struct {
	// Enabled schaltet Heartbeat + Fernsteuerung. nil/unges. = true (Default an).
	Enabled *bool `yaml:"enabled"`
	// Control schaltet NUR die Fernsteuerung (cmd/svc/<device>). nil/unges. = true.
	Control *bool `yaml:"control"`
	// Device ist der Gerätename im Monitor (Default "svc-edgeproxy", muss mit svc- beginnen).
	Device string `yaml:"device"`
	// CustomerNumber ist die Kundennummer des Mandanten im Monitor (z. B. "4111"). Gesetzt →
	// Fernsteuerung auf cmd/svc/<kundennr>/<device ohne svc->; leer → cmd/svc/<…> (nur
	// Eigenbetrieb). Seit 12.11.2026 liest eine Box eines Kunden-Mandanten am Monitor nur noch
	// ihren eigenen Zweig; ohne Kundennummer hat sie dort keine Fernsteuerung (T-SENS-20261107-02.1).
	CustomerNumber string `yaml:"customer_number"`
	// IntervalS ist das Heartbeat-Intervall in Sekunden (Default 30).
	IntervalS int `yaml:"interval_s"`
	// MaxPauseMinutes begrenzt „pause"/„drain" aus der Fernsteuerung (Default 60). Danach läuft
	// der Versand von selbst wieder an — eine vergessene Pause füllte bis 07.11.2026 den Puffer
	// bis zum Tail-Drop, und dann gingen neue Messwerte verloren (Sicherheitsprüfung P3-03).
	MaxPauseMinutes int `yaml:"max_pause_minutes"`
	// IngestURL ist der HTTP-Ingest-Endpunkt für den Heartbeat. Leer = HTTP.Endpoint.
	IngestURL string `yaml:"ingest_url"`
	// Token ist das X-Ingest-Token für den Heartbeat. Leer = HTTP.Token bzw. MQTT.Password.
	Token string `yaml:"token"`
}

// MonitorEnabled meldet, ob die Monitor-Anbindung (Heartbeat) aktiv ist (Default an).
func (c *Config) MonitorEnabled() bool {
	return c.Monitor.Enabled == nil || *c.Monitor.Enabled
}

// MonitorControlEnabled meldet, ob die Fernsteuerung aktiv ist (Default an, setzt
// MonitorEnabled voraus).
func (c *Config) MonitorControlEnabled() bool {
	return c.MonitorEnabled() && (c.Monitor.Control == nil || *c.Monitor.Control)
}

// StorageConf enthält Einstellungen für die persistente lokale Datenbank.
type StorageConf struct {
	// Path definiert das Verzeichnis, in dem die BadgerDB-Dateien abgelegt werden.
	Path string `yaml:"path"`
	// MaxSizeMB definiert die maximale Größe der Datenbank in Megabyte (Tail Drop Limit).
	MaxSizeMB int `yaml:"max_size_mb"`
}

// MQTTConf enthält Konfigurationen für die MQTT-Listener sowie die MQTT-Upstream-Verbindung.
type MQTTConf struct {
	// Bind ist die Adresse, auf der der lokale Broker lauscht (TCP und WebSocket). Standard
	// 127.0.0.1: zigbee2mqtt bzw. die mqttbridge laufen auf derselben Box. Bis 07.11.2026
	// lauschte der Broker fest auf allen Schnittstellen und ohne Anmeldung — wer das Netz der Box
	// erreichte, konnte Messwerte einspeisen, die mit dem echten Token in den Mandanten gingen
	// (Sicherheitsprüfung P3-01/P3-02). Eine andere Adresse verlangt LocalUsers.
	Bind string `yaml:"bind"`
	// LocalUsers sind die Konten am lokalen Broker für Verbindungen, die NICHT von Loopback kommen
	// (z. B. zigbee2mqtt auf einem anderen Rechner: dort mqtt.user/mqtt.password eintragen).
	// Von Loopback geht es ohne Anmeldung (Bridge, Dashboard-Tunnel /mqtt, z2m auf der Box).
	LocalUsers []LocalUser `yaml:"local_users"`
	// ClientID ist die Grundlage der Client-IDs am Upstream (Haupt-Upstream, Routen, Steuerung).
	// Leer = "mlc2af-proxy-" + Instanzkennung (aus /etc/machine-id, sonst Hostname). Bis
	// 07.11.2026 trugen alle Boxen dieselben festen IDs — zwei Boxen am selben Broker warfen sich
	// gegenseitig hinaus, tausende Male je Minute (Sicherheitsprüfung P3-04).
	ClientID string `yaml:"client_id"`
	// LocalPort ist der lokale Port für den TCP Mochi-MQTT Listener (z.B. 1883).
	LocalPort     int              `yaml:"local_port"`
	// WsPort ist der Port des WebSocket-Listeners für das Live-Dashboard (z.B. 1885).
	WsPort          int              `yaml:"ws_port"`
	// Upstream ist die Adresse des Cloud- oder Master-Brokers (z.B. tcp://cloud.example.com:1883).
	Upstream        string           `yaml:"upstream"`
	// Username ist der optionale Benutzername für den Upstream-Broker.
	Username        string           `yaml:"username"`
	// Password ist das optionale Passwort für den Upstream-Broker.
	Password        string           `yaml:"password"`
	// TopicAlias aktiviert die Nutzung von MQTT 5 Topic Aliases zur Bandbreiteneinsparung.
	TopicAlias      bool             `yaml:"topic_alias"`
	// TimestampMode steuert, wie Zeitstempel übertragen werden ("none", "json_inject", "v5_property").
	TimestampMode   string           `yaml:"timestamp_mode"`
	// TimestampField definiert den JSON-Key beim Modus "json_inject" (Standard: "_ts").
	TimestampField  string           `yaml:"timestamp_field"`
	// TopicRewrite erlaubt das Umschreiben von Topics vor dem Senden an den Upstream-Broker.
	TopicRewrite    *TopicRewriteConf `yaml:"topic_rewrite,omitempty"`
	// Filter limitiert, welche Topics lokal gespeichert und an den Upstream gesendet werden.
	Filter          *FilterConf       `yaml:"filter,omitempty"`
	// DeduplicateIntervalMs verwirft Nachrichten mit identischem Topic und Payload,
	// wenn sie innerhalb dieses Zeitfensters eintreffen (Standard: 0 = deaktiviert).
	DeduplicateIntervalMs int         `yaml:"deduplicate_interval_ms"`
	// DeduplicateIgnoreKeys definiert JSON-Keys, die beim intelligenten
	// Deduplizierungs-Vergleich ignoriert werden sollen (z.B. last_seen).
	DeduplicateIgnoreKeys []string           `yaml:"deduplicate_ignore_keys,omitempty"`
	// ZigbeeBaseTopic ist das Basis-Topic von zigbee2mqtt (z2m: mqtt.base_topic) für die
	// Gerätestatistik. Leer = "zigbee2mqtt".
	ZigbeeBaseTopic string `yaml:"zigbee_base_topic,omitempty"`
	// Downstream ermöglicht den Empfang von Nachrichten vom Upstream-Broker
	// und das Forwarden derselben an lokale MQTT-Clients (z.B. Aktor-Steuerung).
	Downstream          *DownstreamConf      `yaml:"downstream_config,omitempty"`
}

// DownstreamConf steuert das bidirektionale Routing vom Upstream-Broker
// zurück zu lokalen MQTT-Clients am Mochi-Broker.
type DownstreamConf struct {
	// SubscribeTopics definiert die Topics, die beim Upstream-Broker abonniert werden sollen.
	// Wildcards sind erlaubt (z.B. "cloud/commands/+").
	SubscribeTopics []string          `yaml:"subscribe_topics,omitempty"`
	// Rewrite erlaubt das Zurückschreiben des Topics vor dem lokalen Publish.
	// (z.B. match_prefix: "cloud/commands/zigbee2mqtt/" → replace_with: "zigbee2mqtt/")
	Rewrite       *TopicRewriteConf `yaml:"rewrite,omitempty"`
}

// FilterConf ermöglicht das Filtern von Topics anhand von Präfixen.
type FilterConf struct {
	// AllowedPrefixes definiert Topics, die gespeichert und gesendet werden dürfen. Wenn gesetzt, müssen Topics hiermit beginnen.
	AllowedPrefixes []string `yaml:"allowed_prefixes"`
	// IgnoredPrefixes definiert Topics, die ignoriert werden sollen.
	IgnoredPrefixes []string `yaml:"ignored_prefixes"`
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

// LocalUser ist ein Konto am lokalen Broker.
type LocalUser struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// ServerConf konfiguriert den Diagnose-Webserver.
type ServerConf struct {
	// Enable steuert, ob der Webserver überhaupt gestartet werden soll (Standard: true).
	Enable bool `yaml:"enable"`
	// Host definiert das Interface, auf dem der Webserver lauscht (Standard: 0.0.0.0 für alle IPs).
	// Ohne Username/Password lauscht das Dashboard nur auf 127.0.0.1 (siehe applySecureDefaults).
	Host string `yaml:"host"`
	// Port ist der Port, unter dem das Dashboard und die API erreichbar sind (Standard: 8080).
	Port int `yaml:"port"`
	// APIPrefix definiert das Präfix für die Diagnose-REST-API (Standard: /api/v1).
	APIPrefix string `yaml:"api_prefix"`
	// Username ist der optionale Benutzername für HTTP Basic Auth (Dashboard & API).
	Username string `yaml:"username"`
	// Password ist das optionale Passwort für HTTP Basic Auth.
	Password string `yaml:"password"`
	// PublicStats: Statistik-Ansicht und ihre API (status, stats, history, devices, health) ohne
	// Anmeldung — der Kunde soll sehen, ob der Proxy läuft und weiterleitet. Geschützt bleiben die
	// Live-Ansicht (MQTT-Inhalte), Konfiguration und Filter. Unges. = true (seit 1.2.0).
	PublicStats *bool `yaml:"public_stats,omitempty"`
	// PublicDevices: Gerätenamen und -zahlen gehören zur öffentlichen Statistik. Unges. = true.
	PublicDevices *bool `yaml:"public_devices,omitempty"`
}

// StatsPublic meldet, ob die Statistik ohne Anmeldung sichtbar ist (unges. = true).
func (s ServerConf) StatsPublic() bool { return s.PublicStats == nil || *s.PublicStats }

// DevicesPublic meldet, ob die Gerätestatistik ohne Anmeldung sichtbar ist (unges. = true;
// nur zusammen mit StatsPublic).
func (s ServerConf) DevicesPublic() bool {
	return s.StatsPublic() && (s.PublicDevices == nil || *s.PublicDevices)
}

// HasLogin meldet, ob Benutzer und Passwort für den geschützten Teil gesetzt sind.
func (s ServerConf) HasLogin() bool { return s.Username != "" && s.Password != "" }

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
	for i := range cfg.Routes {
		if cfg.Routes[i].Mode == "" {
			cfg.Routes[i].Mode = "mqtt"
		}
		if cfg.Routes[i].BaseTopic == "" {
			cfg.Routes[i].BaseTopic = "zigbee2mqtt"
		}
	}
	if cfg.Storage.Path == "" {
		cfg.Storage.Path = "./data"
	}
	if cfg.Storage.MaxSizeMB == 0 {
		cfg.Storage.MaxSizeMB = 1024
	}
	if cfg.MQTT.Bind == "" {
		cfg.MQTT.Bind = "127.0.0.1"
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
	// Default: Webserver aktivieren, falls nicht explizit in der YAML auf "enable: false" gesetzt.
	if !strings.Contains(string(data), "enable: false") {
		cfg.Server.Enable = true
	}

	if cfg.Server.Host == "" {
		cfg.Server.Host = "0.0.0.0"
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

	// Monitor-Anbindung: Defaults. Gerät svc-edgeproxy, 30 s Heartbeat; Heartbeat-Ziel
	// und Token aus dem HTTP-Block bzw. (Fallback) aus den MQTT-Upstream-Zugangsdaten —
	// so funktioniert die Anbindung ohne Extra-Konfiguration „out of the box".
	if cfg.Monitor.Device == "" {
		cfg.Monitor.Device = "svc-edgeproxy"
	}
	// Kundennummer nur Ziffern — sie wird Teil eines Topics (kein „/“, „+“, „#“).
	if n := cfg.Monitor.CustomerNumber; n != "" && !regexp.MustCompile(`^[0-9]{1,12}$`).MatchString(n) {
		return nil, fmt.Errorf("monitor.customer_number %q: nur Ziffern", n)
	}
	if cfg.Monitor.IntervalS == 0 {
		cfg.Monitor.IntervalS = 30
	}
	if cfg.Monitor.MaxPauseMinutes <= 0 {
		cfg.Monitor.MaxPauseMinutes = 60
	}
	if cfg.Monitor.IngestURL == "" {
		cfg.Monitor.IngestURL = cfg.HTTP.Endpoint
	}
	if cfg.Monitor.Token == "" {
		if cfg.HTTP.Token != "" {
			cfg.Monitor.Token = cfg.HTTP.Token
		} else {
			cfg.Monitor.Token = cfg.MQTT.Password // Upstream-Passwort = Ingest-Token
		}
	}

	if err := validateControl(&cfg); err != nil {
		return nil, err
	}
	if err := applySecureDefaults(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// IsLoopbackHost meldet, ob eine Bind-Adresse nur lokal erreichbar ist (127.0.0.0/8, ::1,
// localhost). Leer und 0.0.0.0/:: sind ALLE Schnittstellen — also nicht lokal.
func IsLoopbackHost(host string) bool {
	h := strings.Trim(strings.TrimSpace(host), "[]")
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// applySecureDefaults setzt die Zugangsregeln fail closed durch (Sicherheitsprüfung 07.11.2026):
//   - Lokaler Broker auf einer Nicht-Loopback-Adresse ohne Konten → Start verweigert. Sonst nähme
//     er aus dem Netz jede Nachricht an und leitete sie mit dem echten Token weiter.
//   - Dashboard ohne Passwort → nur auf 127.0.0.1 (statt abzuschalten: Daten laufen weiter, die
//     Diagnose bleibt am Gerät selbst möglich). Mit Passwort auf der eingestellten Adresse.
func applySecureDefaults(cfg *Config) error {
	if !IsLoopbackHost(cfg.MQTT.Bind) {
		n := 0
		for _, u := range cfg.MQTT.LocalUsers {
			if u.Username != "" && u.Password != "" {
				n++
			}
		}
		if n == 0 {
			return fmt.Errorf("mqtt.bind=%q ist von außen erreichbar, aber mqtt.local_users ist leer — "+
				"der lokale Broker nähme jede Nachricht an und leitete sie mit dem Ingest-Token weiter. "+
				"Konto eintragen oder bind: 127.0.0.1", cfg.MQTT.Bind)
		}
	}
	// Ohne Anmeldung: Mit öffentlicher Statistik lauscht der Server weiter auf host — der geschützte
	// Teil (Live, Konfiguration) antwortet dann nur Anfragen von 127.0.0.1 (web.requireAuth). Ohne
	// öffentliche Statistik gibt es nichts Öffentliches; dann nur 127.0.0.1 (wie bis 1.1).
	if cfg.Server.Enable && !IsLoopbackHost(cfg.Server.Host) && !cfg.Server.HasLogin() {
		if cfg.Server.StatsPublic() {
			log.Printf("WARNUNG: Dashboard ohne server.username/password — Statistik öffentlich auf %q, Live-Ansicht und Konfiguration nur von 127.0.0.1", cfg.Server.Host)
		} else {
			log.Printf("WARNUNG: Dashboard ohne server.username/password — lauscht nur auf 127.0.0.1 statt %q", cfg.Server.Host)
			cfg.Server.Host = "127.0.0.1"
		}
	}
	return nil
}

// machineIDPath ist die Quelle der Instanzkennung (im Test ersetzbar).
var machineIDPath = "/etc/machine-id"

// InstanceID liefert eine je Rechner stabile, kurze Kennung: die ersten 12 Zeichen von
// /etc/machine-id, sonst der Hostname, sonst "unknown". Der Hostname allein taugt nicht: Jeder
// Raspberry Pi heißt ab Werk „raspberrypi".
func InstanceID() string {
	if b, err := os.ReadFile(machineIDPath); err == nil {
		id := strings.TrimSpace(string(b))
		if len(id) >= 12 {
			return id[:12]
		}
		if id != "" {
			return id
		}
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "unknown"
}

// UpstreamClientID ist die Basis-Client-ID dieser Box am Upstream.
func (c *Config) UpstreamClientID() string {
	if id := strings.TrimSpace(c.MQTT.ClientID); id != "" {
		return id
	}
	return "mlc2af-proxy-" + InstanceID()
}
