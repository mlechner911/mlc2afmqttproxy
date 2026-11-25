# Architektur: Go-Proxy

Der Go-Proxy ist das Herzstück des ausfallsicheren Zigbee-Gateways. Er ist dafür verantwortlich, lokal Daten entgegenzunehmen, zwischenzuspeichern und an einen Cloud-Dienst weiterzuleiten (Store & Forward).

## Module
Das Backend ist in folgende Logik-Bausteine unterteilt:

- **`pkg/config`**: Laden der YAML-Konfiguration (Betriebsart `mqtt` oder `http`).
- **`pkg/broker`**: Eingebetteter **Mochi MQTT Broker**, der auf einem lokalen Port lauscht. Hier sendet Zigbee2MQTT seine Telemetrie-Daten hin.
- **`pkg/storage`**: Ein **BadgerDB** Adapter. Dieser SSD-optimierte Key-Value-Store speichert Nachrichten zwischen, falls der Upstream (Cloud/Internet) nicht erreichbar ist.
- **`pkg/forwarder`**: Die Upstream-Schnittstelle. Erlaubt den Versand von Daten entweder direkt an einen Cloud MQTT Broker (Paho Client) oder an einen HTTP Ingest Endpoint.
- **`pkg/metrics`**: Zähler seit dem Start (empfangen, gefiltert je Regel, weitergeleitet …) und Merker (Puffer voll, letzte Nachricht).
- **`pkg/history`**: Verlaufsstatistik — misst alle 15 s die Zähler, verbucht die Differenzen in 10-Minuten-Abschnitten (31 Tage) und Tageswerten (400 Tage), führt Zustandsintervalle der Weiterleitung (ok, Upstream weg, pausiert, Puffer voll) und die Nachrichten je Gerät. JSON-Dateien unter `<storage.path>/history/`.
- **`pkg/devices`**: Beobachtet die Zigbee-Geräte (Liste aus `zigbee2mqtt/bridge/devices`, Verfügbarkeit, Batterie, Linkqualität, zuletzt gesehen) — als Beobachter am Broker, vor Filter und Deduplizierung.
- **`pkg/svcmon`**: Heartbeat an den MLC Sensor Monitor (Zustand, Puffer, Verbindungen, Version) und Fernsteuerung.
- **`pkg/web`**: **Gin-Webserver** mit öffentlicher Statistik (`/status`, `/history`, `/devices`) und geschütztem Teil (Live-Tunnel `/mqtt`, `/config`, `/filters`; Session-Cookie oder Basic Auth). Oberfläche in `pkg/web/static`: Vanilla-JS-Module, Tailwind (Standalone-CLI, `task web:css`, Eingabe `web/app.css`), Diagramme mit ml-time-graph (vendored, `task web:vendor`).

## Datenfluss (Upstream)
1. `Zigbee2MQTT` -> `Mochi Broker` (Lokal)
2. Proxy schreibt die Daten sofort in `BadgerDB`
3. Ein Forward-Worker liest kontinuierlich aus `BadgerDB`
4. Der Forward-Worker sendet die Daten über das `Forwarder` Interface (MQTT oder HTTP) an die Cloud.
5. Bei Erfolg wird der Eintrag aus der BadgerDB gelöscht (FIFO-Prinzip).
