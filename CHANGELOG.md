# Changelog

One line per change. Background, measurements and ticket references: release notes in the project's Doc Hub (`releases/<version>.md`).

## [1.1.0] - 2026-11-07

**Upgrade:** the local broker now listens on `127.0.0.1` only. If zigbee2mqtt runs on another host, set `mqtt.bind` and an account in `mqtt.local_users` — otherwise the proxy refuses to start. Without `server.username`/`server.password` the dashboard is reachable on `127.0.0.1` only.

### Added
- Routes: additional upstreams, each with its own credentials, buffer, filter and device list.
- Self-monitoring: heartbeat to the MLC Sensor Monitor and remote pause/resume/restart/stop (`monitor.enabled`).
- Downstream routing from the upstream to local clients.
- Settings `mqtt.bind`, `mqtt.local_users`, `mqtt.client_id`, `monitor.max_pause_minutes`.
- Permanently rejected messages are kept in `dead-letter.jsonl` instead of being deleted.
- Counters for dead-lettered and tail-dropped messages in `/api/v1/stats`.
- `--version`; the version now comes from the `VERSION` file.
- systemd units for running from the repository (`deploy/systemd/`).

### Fixed
- Buffer entries could stay stuck until restart, leave out of order, or overwrite each other.
- Tail drop measured the database file instead of the pending buffer.
- 100 % CPU from tombstone scans in the buffer.
- MQTT reason code 0x10 (no subscribers) was treated as rejection and the message deleted.
- One permanently rejected message (MQTT ≥ 0x80, HTTP 400/413) stalled all forwarding.
- Invalid JSON in HTTP mode was dropped silently and counted as forwarded.
- Remote `restart` exited with code 0, so the package unit did not restart.
- A route lost a renamed device; it now follows the IEEE address.
- Packages carried version 1.0.2 regardless of the release.

### Security
- The local broker accepted unauthenticated connections on all interfaces.
- The WebSocket port accepted any origin; `/mqtt` now requires the same origin.
- Dashboard: `Access-Control-Allow-Origin: *` removed, security headers added, stored XSS fixed.
- Retained remote-control commands are ignored; a pause ends after `monitor.max_pause_minutes`.
- All boxes used the same MQTT client IDs and kicked each other off the broker.
- Packages contained the local `config.yaml` with credentials; they now ship the example, mode 0640.
- Messages above 1 MiB are refused; the deduplication list no longer grows without bound.
- 15 reachable vulnerabilities fixed (Go 1.26.10, x/net, quic-go, compress).
- Removed a revoked ingest token from `config.example.yaml`.

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
