package forwarder

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// HTTPForwarder implementiert die Forwarder-Schnittstelle über HTTP-POST Requests.
// Er konvertiert flache Zigbee2MQTT JSON-Payloads in das MLC Sensor Monitor Format
// und sendet diese an die konfigurierte Ingest-Schnittstelle.
type HTTPForwarder struct {
	// Endpoint ist die Ziel-URL der HTTP Ingest-API
	Endpoint string
	// Token ist das Authentifizierungstoken für den "X-Ingest-Token"-Header
	Token    string
	// client ist der interne HTTP-Client mit Timeout
	client   *http.Client
}

// IngestReading entspricht einer einzelnen Messung im MLC Sensor Monitor Format.
type IngestReading struct {
	// Key ist der Name des Messwerts (z.B. "temperature", "humidity")
	Key   string  `json:"key"`
	// Value ist der numerische Messwert (Fließkommazahl)
	Value float64 `json:"value"`
	// TS ist der RFC3339-Zeitstempel der Messung (wichtig für historische Offline-Daten)
	TS    string  `json:"ts,omitempty"`
}

// IngestRequest repräsentiert das gesamte JSON-Payload für die Ingest-Schnittstelle
// des MLC Sensor Monitors.
type IngestRequest struct {
	// Device identifiziert das Quellgerät (z.B. "living_room")
	Device   string          `json:"device"`
	// Readings enthält das Array der konkreten Messwerte des Geräts
	Readings []IngestReading `json:"readings"`
}

// NewHTTPForwarder erstellt eine neue Instanz des HTTPForwarder.
// Der interne http.Client wird mit einem standardmäßigen Timeout von 10 Sekunden konfiguriert.
func NewHTTPForwarder(endpoint, token string) *HTTPForwarder {
	return &HTTPForwarder{
		Endpoint: endpoint,
		Token:    token,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Connect implementiert das Forwarder-Interface. Für HTTP ist kein dauerhafter Verbindungsaufbau nötig.
func (f *HTTPForwarder) Connect() error {
	log.Printf("HTTP Forwarder initialisiert für Endpoint: %s", f.Endpoint)
	return nil
}

// IsConnected gibt immer true zurück, da HTTP verbindungslos arbeitet.
// Fehler werden erst beim eigentlichen Request (Send) festgestellt.
func (f *HTTPForwarder) IsConnected() bool {
	return true
}

// Send nimmt eine Nachricht entgegen, filtert numerische und boolesche Werte
// aus dem flachen Zigbee2MQTT JSON-Payload, transformiert diese in das MLC-spezifische
// IngestRequest-Format und sendet sie per POST an den Ingest-Endpunkt.
// Sollte die API einen Fehlercode ungleich 2xx zurückgeben oder die Verbindung offline sein,
// schlägt die Methode fehl, sodass der Worker das Element im Puffer behält.
func (f *HTTPForwarder) Send(topic string, payload []byte, timestamp time.Time) error {
	// 1. Device ermitteln (z.B. "zigbee2mqtt/living_room" -> "living_room")
	parts := strings.Split(topic, "/")
	device := parts[len(parts)-1]

	// 2. Payload parsen
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		log.Printf("[HTTP-Ingest] Ungültiges JSON im Payload (%s), ignoriert.", device)
		return nil // Verwerfen, da nicht reparierbar (gibt kein Fehler an Worker zurück, um Blockade zu verhindern)
	}

	// 3. Konvertierung der flachen Struktur in Ingest Readings
	timeStr := timestamp.UTC().Format(time.RFC3339)
	var readings []IngestReading

	for k, v := range raw {
		var floatVal float64
		switch val := v.(type) {
		case float64:
			floatVal = val
		case bool:
			// Bools werden in 1 oder 0 übersetzt
			if val {
				floatVal = 1
			} else {
				floatVal = 0
			}
		default:
			continue // Nicht-numerische Werte wie Strings (z.B. Verbindungsstatus) ignorieren
		}
		
		readings = append(readings, IngestReading{
			Key:   k,
			Value: floatVal,
			TS:    timeStr, // Sendet den Erstellungszeitstempel der Nachricht anstatt die aktuelle Serverzeit
		})
	}

	// Falls keine passenden Metriken gefunden wurden, verwerfen wir das Paket
	if len(readings) == 0 {
		return nil
	}

	reqBody := IngestRequest{
		Device:   device,
		Readings: readings,
	}

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("fehler beim generieren des Ingest-Requests: %v", err)
	}

	// 4. POST Request erstellen und Auth-Header anfügen
	req, err := http.NewRequest(http.MethodPost, f.Endpoint, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	if f.Token != "" {
		// Nutzt X-Ingest-Token zur Authentifizierung
		req.Header.Set("X-Ingest-Token", f.Token)
	}

	// Request ausführen
	resp, err := f.client.Do(req)
	if err != nil {
		return err // Netzwerk-Timeout / Offline
	}
	defer resp.Body.Close()

	// Antwort auswerten
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("[HTTP-Ingest] Fehler %d vom Server. Behalte im Puffer...", resp.StatusCode)
		return fmt.Errorf("upstream returned http %d", resp.StatusCode)
	}

	log.Printf("[HTTP-Ingest] Erfolgreich gesendet: %d Readings für '%s' (TS: %s)", len(readings), device, timeStr)
	return nil
}

// Close schließt alle idle Verbindungen des HTTP-Clients.
func (f *HTTPForwarder) Close() {
	f.client.CloseIdleConnections()
}

