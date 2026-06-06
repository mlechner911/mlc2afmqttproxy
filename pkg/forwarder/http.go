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

type HTTPForwarder struct {
	Endpoint string
	Token    string
	client   *http.Client
}

// IngestReading entspricht dem MLC Sensor Monitor Format
type IngestReading struct {
	Key   string  `json:"key"`
	Value float64 `json:"value"`
	TS    string  `json:"ts,omitempty"`
}

// IngestRequest entspricht dem MLC Sensor Monitor Format
type IngestRequest struct {
	Device   string          `json:"device"`
	Readings []IngestReading `json:"readings"`
}

// NewHTTPForwarder erstellt einen neuen HTTP Client.
func NewHTTPForwarder(endpoint, token string) *HTTPForwarder {
	return &HTTPForwarder{
		Endpoint: endpoint,
		Token:    token,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (f *HTTPForwarder) Connect() error {
	log.Printf("HTTP Forwarder initialisiert für Endpoint: %s", f.Endpoint)
	return nil
}

func (f *HTTPForwarder) IsConnected() bool {
	return true
}

// Send führt einen HTTP POST Request auf den Ingest-Endpoint aus und mappt das Zigbee Format.
func (f *HTTPForwarder) Send(topic string, payload []byte, timestamp time.Time) error {
	// 1. Device ermitteln (z.B. "zigbee2mqtt/living_room" -> "living_room")
	parts := strings.Split(topic, "/")
	device := parts[len(parts)-1]

	// 2. Payload unmarshals
	var raw map[string]interface{}
	if err := json.Unmarshal(payload, &raw); err != nil {
		log.Printf("[HTTP-Ingest] Ungültiges JSON im Payload (%s), ignoriert.", device)
		return nil // Verwerfen
	}

	// 3. Mapping in Readings
	timeStr := timestamp.UTC().Format(time.RFC3339)
	var readings []IngestReading

	for k, v := range raw {
		var floatVal float64
		switch val := v.(type) {
		case float64:
			floatVal = val
		case bool:
			if val {
				floatVal = 1
			} else {
				floatVal = 0
			}
		default:
			continue // Strings etc. ignorieren
		}
		
		readings = append(readings, IngestReading{
			Key:   k,
			Value: floatVal,
			TS:    timeStr, // Absolutes Killer-Feature für Store & Forward
		})
	}

	if len(readings) == 0 {
		// Keine passenden Metriken gefunden -> als "Erfolgreich" abstempeln, damit es gelöscht wird
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

	// 4. Senden
	req, err := http.NewRequest(http.MethodPost, f.Endpoint, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	if f.Token != "" {
		// Angepasst nach Doku: Nutzt X-Ingest-Token!
		req.Header.Set("X-Ingest-Token", f.Token)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return err // Timeout / Offline
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("[HTTP-Ingest] Fehler %d vom Server. Behalte im Puffer...", resp.StatusCode)
		return fmt.Errorf("upstream returned http %d", resp.StatusCode)
	}

	log.Printf("[HTTP-Ingest] Erfolgreich gesendet: %d Readings für '%s' (TS: %s)", len(readings), device, timeStr)
	return nil
}

func (f *HTTPForwarder) Close() {
	f.client.CloseIdleConnections()
}
