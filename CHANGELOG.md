# Changelog

Alle signifikanten Änderungen an diesem Projekt werden in dieser Datei dokumentiert.

## [1.0.2] - 2026-06-07

### Hinzugefügt
- **Disk Full Protection (Tail Drop)**: Überwachung der BadgerDB-Festplattenbelegung. Bei Erreichen eines einstellbaren Limits (Default: 1024 MB) blockt der Proxy neue Nachrichten, um eine Betriebssystem-Korruption (Raspberry Pi) bei vollen SD-Karten zuverlässig zu verhindern.
- **Bidirektionale MQTT Bridge**: Das mitgelieferte CLI-Hilfstool (`bin/mqttbridge`) unterstützt nun ein `--bidi` Flag für bidirektionales Routing (`Master <-> Slave`) inklusive Thread-sicherer Loop-Detection (Schutz vor Echo-Loops).

## [1.0.0] - 2026-06-07

### Hinzugefügt
- **MQTT 5 Topic Aliases**: Reduziert massiv den Payload-Overhead durch Ersetzen langer Topics mit Integern (inklusive automatischer Upstream-Restart Erkennung).
- **Intelligente JSON Deduplizierung**: Filtert redundante Sensordaten (Debouncing) basierend auf dem exakten Payload. Unterstützung für optionale `ignore_keys` (z.B. `last_seen`, `linkquality`).
- **Poison Message Protection**: Worker stoppt Endlosschleifen automatisch, wenn der Upstream-Broker Nachrichten aufgrund fehlerhafter Topics oder Payloads abweist (`Reason Code != 0` oder Wildcards in Publish-Topics).
- **Graceful Shutdown**: Sicheres Herunterfahren der Webserver-Routinen und der BadgerDB beim Empfang von OS-Signalen (`SIGTERM`/`SIGINT`), was aktiv vor Datenbankkorruption schützt.
- **Dashboard Absicherung (Security)**: Der Diagnose-Webserver (`:8097`) kann nun über `server.enable: false` komplett deaktiviert oder durch HTTP Basic Auth (`username` & `password`) abgesichert werden (inklusive Websocket-Protection).
- **Paketierung (DEB/RPM)**: Release-Builds via nfpm/goreleaser für native Linux-Installation als systemd-Service.

### Geändert
- **Worker Refactoring**: Deutlich robustere Queue-Abwicklung und sauberere BadgerDB-Integration.
- **Konfigurationsdatei**: Die `config.yaml` wurde um zahlreiche neue Optionen erweitert (`deduplicate_interval_ms`, `topic_alias`, `server.enable`, `server.username` etc.).
