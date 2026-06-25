# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Added
- **Self-monitoring & remote control (optional, on by default):** The proxy registers with the MLC Sensor Monitor as a service `svc-edgeproxy` via a **heartbeat** (HTTP ingest: `service_up`/`service_state`/`svc_uptime_s`) — if it goes down, the monitor raises an event automatically (the Zigbee edge path is fully monitored). It also accepts **remote control** on `cmd/svc/<name>` over the already-used upstream broker: `pause`/`drain` (upstream forwarding halts, data stays buffered — no loss), `resume` (flush the buffer), `restart`/`stop` (graceful exit). Deliberately **loosely coupled** (no import of monitor packages, plain HTTP+MQTT) and fully disableable via `monitor.enabled: false` — the proxy keeps running without the monitor. New package `pkg/svcmon`, config section `monitor:`, worker pause gate (`SetPauseCheck`).

## [1.0.2] - 2026-06-07

### Added
- **Disk Full Protection (Tail Drop)**: Monitoring of BadgerDB disk usage. Upon reaching a configurable limit (Default: 1024 MB), the proxy silently drops new messages to reliably prevent OS corruption (e.g. on Raspberry Pi SD cards) from disk exhaustion.
- **Bidirectional MQTT Bridge**: The included CLI helper tool (`bin/mqttbridge`) now supports a `--bidi` flag for bidirectional routing (`Master <-> Slave`) with thread-safe loop detection to prevent echo loops.

## [1.0.0] - 2026-06-07

### Added
- **MQTT 5 Topic Aliases**: Massively reduces payload overhead by replacing long topics with integers (includes automatic upstream restart detection).
- **Smart JSON Deduplication**: Filters redundant sensor data (debouncing) based on the exact payload. Supports optional `ignore_keys` (e.g., `last_seen`, `linkquality`).
- **Poison Message Protection**: Worker automatically stops endless loops if the upstream broker rejects messages due to invalid topics or payloads (`Reason Code != 0` or wildcards in publish topics).
- **Graceful Shutdown**: Safe shutdown of web server routines and BadgerDB upon receiving OS signals (`SIGTERM`/`SIGINT`), actively protecting against database corruption.
- **Dashboard Security**: The diagnostic web server (`:8097`) can now be completely disabled via `server.enable: false` or secured with HTTP Basic Auth (`username` & `password`), including websocket protection.
- **Packaging (DEB/RPM)**: Release builds via nfpm/goreleaser for native Linux installation as a systemd service.

### Changed
- **Worker Refactoring**: Significantly more robust queue processing and cleaner BadgerDB integration.
- **Configuration File**: The `config.yaml` has been expanded with numerous new options (`deduplicate_interval_ms`, `topic_alias`, `server.enable`, `server.username`, etc.).
