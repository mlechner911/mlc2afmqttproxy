# MLC2AF MQTT Store & Forward Proxy

Ein robuster Edge-Proxy für IoT-Sensoren (z.B. Zigbee2MQTT), der auf unzuverlässigen Verbindungen (z.B. Edge-Gateways mit Mobilfunk/schlechtem WLAN) für eine verlustfreie Telemetrie-Übertragung in die Cloud sorgt.

## Was macht der Proxy?
Der Proxy startet einen lokalen MQTT Broker (Mochi), an den Zigbee2MQTT (oder andere lokale Sensoren) ihre Daten senden. Der Proxy fängt diese Daten ab und schreibt sie lokal und extrem schnell auf die Festplatte (BadgerDB). Ein unabhängiger Hintergrund-Worker versucht anschließend, diese Daten an die Cloud weiterzuleiten.

### Kern-Features
* **Store & Forward**: Fällt das Internet am Gateway aus, werden alle Nachrichten lokal in der Warteschlange gepuffert. Nichts geht verloren.
* **Offline Catch-Up (Time-Travel)**: Die exakten historischen Zeitstempel (`ts`) des Sensor-Ereignisses werden bei der Übertragung nachgereicht. So entstehen keine verfälschten Graphen, wenn das Internet wiederkehrt.
* **Zwei Upstream-Modi**: 
  * `http`: Mappt flaches Zigbee-JSON direkt auf das strukturierte MLC Sensor Monitor Format (`POST /api/v1/ingest`).
  * `mqtt`: Leitet MQTT-Nachrichten mit `QoS 1` an einen vorgeschalteten Cloud-Broker weiter.
* **Health & Diagnostik**: Eingebautes Web-Dashboard (Port `8097`) zeigt den Live-Pufferstand und Status-Informationen an (`/api/v1/health` liefert die Version).
* **Ausfallsicher**: Out-of-the-Box Systemd-Deployment für Autostart nach Stromausfällen.

## Quickstart
Das Projekt nutzt [Task](https://taskfile.dev/) für automatisierte Builds.

1. **Abhängigkeiten auflösen:** `task setup`
2. **Kompilieren:** `task build`
3. **Lokal starten:** `task run`
4. **Als Hintergrunddienst (Linux) installieren:** `task install-service` (erfordert sudo)

## Konfiguration
Die Steuerung erfolgt vollständig über die `config.yaml` Datei.
Alle Parameter und Architektur-Diagramme findest du in der [Konfigurations-Doku](docs/configuration.md).

## Versionierung
Die Version des Proxys wird beim Bauen (`task build`) automatisch aus deinen Git-Tags abgeleitet und fest in das Binary kompiliert. Sie kann jederzeit über den `GET /api/v1/health` Endpunkt abgefragt werden.
