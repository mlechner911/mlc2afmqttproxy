# Konfiguration (`config.yaml`)

Der Proxy wird vollständig über eine zentrale `config.yaml` Datei gesteuert. Diese Datei muss im selben Verzeichnis wie das `proxy` Binary liegen.

## Konfigurations-Parameter

```yaml
# Die Betriebsart des Upstreams.
# Zulässige Werte: "http" oder "mqtt"
mode: "http"

server:
  # Der Port für das Diagnostik-Web-Dashboard und die Health-API
  port: 8097

storage:
  # Pfad, in dem die BadgerDB ihre Daten ablegt (Store & Forward Puffer)
  path: "./data"

mqtt:
  # Der lokale Port, auf dem der eingebettete Mochi-Broker für Zigbee2MQTT lauscht
  local_port: 1884
  # Upstream-Einstellungen (nur relevant wenn mode: "mqtt")
  upstream: "tcp://cloud-broker.example.com:1883"
  username: "user"
  password: "password"

http:
  # Upstream-Einstellungen (nur relevant wenn mode: "http")
  endpoint: "https://api.example.com/ingest"
  # Optional: Wird als "Authorization: Bearer <token>" Header mitgesendet
  token: "my-secret-token"
```

## Architektur-Diagramm nach Betriebsart

Die Einstellung `mode` entscheidet darüber, über welchen Weg der Worker die Daten an die Cloud überträgt. Der lokale Datenfluss (Zigbee2MQTT -> Mochi -> BadgerDB) bleibt dabei immer identisch.

```mermaid
graph TD
    subgraph Lokales Gateway
        Z[Zigbee2MQTT] -- "Publish (QoS 1)" --> M(Lokaler Broker\nPort 1884)
        M -- "Store Hook" --> DB[(BadgerDB\n./data)]
        DB -- "FIFO Polling" --> W((Forward Worker))
        W --> C{Config: mode}
    end

    subgraph Cloud / Upstream
        C -- "mode: mqtt" --> F_MQTT[Paho MQTT Client]
        F_MQTT -- "tcp://cloud-broker...:1883" --> CB[Cloud MQTT Broker]
        
        C -- "mode: http" --> F_HTTP[net/http Client]
        F_HTTP -- "POST /ingest" --> API[Cloud REST API]
    end
    
    classDef internal fill:#f9f,stroke:#333,stroke-width:2px;
    classDef external fill:#bbf,stroke:#333,stroke-width:2px;
    class M,DB,W,C internal;
    class CB,API external;
```
