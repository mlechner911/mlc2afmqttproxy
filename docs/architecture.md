# Architektur: Go-Proxy

Der Go-Proxy ist das Herzstück des ausfallsicheren Zigbee-Gateways. Er ist dafür verantwortlich, lokal Daten entgegenzunehmen, zwischenzuspeichern und an einen Cloud-Dienst weiterzuleiten (Store & Forward).

## Module
Das Backend ist in folgende Logik-Bausteine unterteilt:

- **`pkg/config`**: Laden der YAML-Konfiguration (Betriebsart `mqtt` oder `http`).
- **`pkg/broker`**: Eingebetteter **Mochi MQTT Broker**, der auf einem lokalen Port lauscht. Hier sendet Zigbee2MQTT seine Telemetrie-Daten hin.
- **`pkg/storage`**: Ein **BadgerDB** Adapter. Dieser SSD-optimierte Key-Value-Store speichert Nachrichten zwischen, falls der Upstream (Cloud/Internet) nicht erreichbar ist.
- **`pkg/forwarder`**: Die Upstream-Schnittstelle. Erlaubt den Versand von Daten entweder direkt an einen Cloud MQTT Broker (Paho Client) oder an einen HTTP Ingest Endpoint.
- **`pkg/web`**: Ein **Gin-Webserver** für Health-Checks und eine einfache Bootstrap 5 **Diagnostik-UI**, um live auf dem Gateway den Systemstatus und den Füllstand der BadgerDB einzusehen.

## Datenfluss (Upstream)
1. `Zigbee2MQTT` -> `Mochi Broker` (Lokal)
2. Proxy schreibt die Daten sofort in `BadgerDB`
3. Ein Forward-Worker liest kontinuierlich aus `BadgerDB`
4. Der Forward-Worker sendet die Daten über das `Forwarder` Interface (MQTT oder HTTP) an die Cloud.
5. Bei Erfolg wird der Eintrag aus der BadgerDB gelöscht (FIFO-Prinzip).
