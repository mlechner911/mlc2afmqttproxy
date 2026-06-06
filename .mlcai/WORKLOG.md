# Worklog: mlc2afmqttproxy

# Worklog

- **2026-06-06**: Umbau des Dashboards auf Svelte & Vite. Echter rekursiver MQTT-Tree-Explorer integriert.
- **2026-06-06**: Mochi-MQTT WebSocket-Listener hinzugefügt und über Gin Reverse-Proxy (`/mqtt`) vor CORS/Firewall-Problemen geschützt.
- **2026-06-06**: UI Styling komplett auf IDE-Theme (Dark Mode, JetBrains Mono, Inter) gewechselt.
- **2026-06-06**: `templates/` Ordner gelöscht, Svelte Frontend in `.gitignore` eingetragen.

## 2026-06-06
- **Basis-Architektur implementiert**:
  - Go Modul initialisiert.
  - `pkg/config`: Konfiguration via `yaml` hinzugefügt (Unterstützung für `mqtt` und `http` Modus).
  - `pkg/broker`: Eingebetteter Mochi MQTT Broker (TCP Listener).
  - `pkg/storage`: BadgerDB Adapter für lokales Store & Forward.
  - `pkg/forwarder`: Upstream-Interface für HTTP und MQTT definiert.
  - `pkg/web`: Gin-Server für Health-API und einfaches HTML/Bootstrap 5 Dashboard angelegt.
- **Forward-Worker & Mochi-Hook implementiert**:
  - Mochi Broker um `StoreHook` erweitert, der alle eingehenden Publish-Nachrichten abfängt und inkl. Topic als JSON in die BadgerDB legt. Die Sortierung erfolgt per Zeitstempel (FIFO).
  - `pkg/worker` implementiert: Eine Hintergrund-Goroutine pollt periodisch die BadgerDB, liest das älteste Element und sendet es via `Forwarder`. Bei Erfolg wird es gelöscht.
- **Upstream & Payload-Transformation**:
  - `HTTPForwarder` packt flaches Zigbee2MQTT JSON in das strukturierte `IngestRequest` Format um.
  - Worker extrahiert das Einspeise-Datum aus der BadgerDB und reicht es als historischen Zeitstempel (`ts`) weiter (Offline Catch-Up).
  - Auth im HTTP Header auf `X-Ingest-Token` angepasst.
  - `MQTTForwarder` mit Paho implementiert (QoS 1).
- **Deployment & Ops**:
  - Web-Dashboard dynamisiert (liest Live-Puffer aus DB).
  - `Taskfile.yml` eingerichtet. Versionierung via Git-Tag an `ldflags` angebunden.
  - `proxy.service` für Systemd vorbereitet und `task install-service` angelegt.
- **CLI-Tools**:
  - `cmd/mqttbridge` CLI-Tool implementiert für einfaches Forwarding von Master- zu Slave-Brokern (mit --help, --version via ldflags).
- **Web-Dashboard Upgrade (Svelte)**:
  - HTML/Bootstrap Dashboard durch moderne **Vite + Svelte + TypeScript** Single Page Application ersetzt.
  - Mochi Broker um **WebSocket-Listener** auf Port 1885 erweitert.
  - Das Dashboard verbindet sich nun live via WebSockets zum MQTT Broker und zeigt den kompletten, laufend aktualisierten MQTT-Tree an.
- **Dokumentation**: `docs/plan.md`, `docs/architecture.md`, `docs/configuration.md` und `README.md` aktualisiert.
