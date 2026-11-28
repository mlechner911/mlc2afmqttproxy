# Operation: usage, authentication, backup, update

[🇩🇪 Deutsch](betrieb.md) · Configuration in detail: [configuration_en.md](configuration_en.md) ·
Monitoring, remote control, switching devices: [backend_en.md](backend_en.md)

This page covers **running** the proxy — for any upstream (your own MQTT broker, an HTTP
endpoint). Heartbeat, remote control and return path for a backend of your own are on
[backend_en.md](backend_en.md).

## How data flows

```
Zigbee devices → zigbee2mqtt → proxy's local broker (:1884) → buffer (BadgerDB, ./data)
                                                                → worker → upstream
                                                                → per route: own buffer → own upstream
upstream → cmd/zb/…/set → return path → zigbee2mqtt/<device>/set → device, ack …/ack back
```

- **Return path:** commands from the upstream go straight to zigbee2mqtt without a buffer; what
  cannot happen right away is rejected ([backend_en.md › Return path](backend_en.md#3b--return-path-switching-devices)).

- zigbee2mqtt (or any MQTT client) **publishes to the proxy**, not directly to the upstream. In z2m:
  `mqtt.server: mqtt://<proxy-host>:1884`.
- Every message is **written to the on-disk buffer first**, with its **receive time**. Only then
  does the worker try to forward it. If the upstream is unreachable, it stays in the buffer — even
  across a restart or power loss — and is sent later with its **original time**
  (`mqtt.timestamp_mode`).
- A message is deleted only once the upstream has accepted it. If the upstream rejects it
  **permanently** — only for format errors: MQTT 0x90 invalid topic, 0x95 too large, 0x99 payload
  format; HTTP 400, 413, 415, 422; invalid JSON — it is written to
  **`<storage.path>/dead-letter.jsonl`** (per route in the route's buffer; with receive time, topic,
  reason and payload) and only then removed from the buffer; the rest continues. A dead-lettered
  measurement is missing at the upstream — it is a finding someone has to look at
  (`messages_dead_lettered_total` in `stats`). If the file cannot be written, the message stays in
  the buffer.
- Any other rejection — including "not authorized" (revoked token, taken-over connection) — counts
  as temporary: the message stays and the proxy retries with growing intervals. Better a stalled
  forwarding than vanished measurements.
- Largest accepted message 1 MiB. When the buffer is full (`storage.max_size_mb`) the proxy accepts
  nothing more (tail drop); those values are missing — counted in
  `messages_dropped_buffer_full_total`.

## Authenticating to the upstream

The proxy authenticates the way the upstream requires. In general:

| Upstream | In `config.yaml` | Meaning |
| :-- | :-- | :-- |
| MQTT broker | `mqtt.upstream`, `mqtt.username`, `mqtt.password` | user and password as checked by the broker |
| HTTP endpoint | `http.endpoint`, `http.token` | token as header `X-Ingest-Token` |
| Additional targets (routes) | `routes[].upstream`, `.username`, `.password` | own credentials per route |

- **What user and password mean is up to the upstream.** A typical Mosquitto broker checks both
  against its password file; other backends may evaluate only the password as an access token.
- **Wrong or revoked credentials:** the upstream refuses the connection. The proxy **keeps
  buffering** (nothing is lost), retries with growing intervals (`worker.retry_min_s` …
  `retry_max_s`) and logs it (`journalctl -u mlc-edge-proxy`). On the dashboard
  `messages_forward_failed_total` rises and the buffer grows. With the right credentials and a
  restart it drains.
- **Changing credentials:** create the new credential at the upstream, put it into `config.yaml`,
  restart, verify (buffer drains, no errors), **then** revoke the old one — so there is never a
  moment without a valid credential.
- **Credentials belong only in `config.yaml`** — never in `config.example.yaml`, never in the
  repository (`config.yaml` is in `.gitignore`). The repository is public.

## Usage

| What | How |
| :-- | :-- |
| Start, stop, restart | `sudo systemctl restart mlc-edge-proxy` (unit: `deployment/proxy.service` or `deploy/systemd/`) |
| Running, which version? | `curl http://127.0.0.1:8097/api/v1/health` → `{"status":"ok","version":"…"}` |
| Is it forwarding? | `curl http://127.0.0.1:8097/api/v1/status` → `state` (`ok`, `upstream_down`, `paused`, `buffer_full`), `buffer_pending`, `last_forward_age_s`, `zigbee_connected` |
| Buffer and counters | `curl http://127.0.0.1:8097/api/v1/stats` → `buffer_count`, `messages_forwarded_total`, `messages_forward_failed_total`, `messages_filtered_total` … |
| History | `http://<host>:8097/` — statistics per day/week/month/year, interruptions, Zigbee devices (no login); API `/api/v1/history`, `/api/v1/devices` |
| Live tree of MQTT topics | **Live** tab — with `server.password`; without a password on the device itself only |
| Log | `journalctl -u mlc-edge-proxy -f` |
| Pause/resume forwarding | via the upstream broker (`cmd/svc/<name>`), see [backend_en.md › Remote control](backend_en.md#3a--remote-control) |

**Reading the buffer:** `buffer_count` 0 = everything forwarded. If it grows while
`messages_forward_failed_total` stays flat, forwarding is paused — otherwise the proxy cannot reach
the upstream (network, credentials). If the buffer space is exhausted (`storage.max_size_mb`), the
proxy accepts **no new** messages rather than filling the SD card ("tail drop").


**History and devices** are stored under `<storage.path>/history/` (JSON; 10-minute values for 31
days, daily values and interruptions for 400 days, `devices.json` with "last seen"). Sampled every
15 s, written every minute and on shutdown. A broken file is moved aside as `.broken`; the proxy
keeps running. A device counts as **silent** when it has not reported for three times its usual
interval (last 7 days, at least 1 h).

## Backing up the configuration

`config.yaml` holds the credentials and is **not** in the repository. If it is lost, the proxy will
not start on the next restart — the running instance does not notice; only the restart (or a power
cut) does.

- After each change, keep a copy **outside** the repository directory, readable only by the
  service user, e.g.:
  ```bash
  install -d -m 700 ~/.config/mlc-edge-proxy
  install -m 600 config.yaml ~/.config/mlc-edge-proxy/config.yaml.$(date +%F)
  ```
- **No backup copies inside the repository directory** (`config.yaml.bak` …): `.gitignore` only
  covers `config.yaml`; a `git add -A` would push the copy with its credentials to the public
  repository.

## Updating

- **Never** run `git checkout`, `git switch` or `git pull` in the directory the service runs from.
  Git overwrites **ignored** files on a switch without warning — including `config.yaml`. (That is
  how the configuration was lost on the development server on 7 Nov 2026.)
- Instead install the package (`task package` → `build/dist/*.deb|.rpm`), or build in a **separate**
  directory and only replace the binary:
  ```bash
  git worktree add /tmp/proxy-build main && cd /tmp/proxy-build && task build-server
  install -m 755 bin/mlc-edge-proxy /path/to/service/bin/mlc-edge-proxy
  sudo systemctl restart mlc-edge-proxy
  ```
- The buffer survives the update. Entries still buffered in the **old format** (before 7 Nov 2026:
  time as key) are forwarded first after the update, with their time.
- After the restart: `health` (version), `stats` (no errors, buffer drains), log.

## Security — operating conditions

- **The local broker (`:1884`, WebSocket `:1885`) listens on `127.0.0.1` only** (`mqtt.bind`).
  Whatever arrives there is forwarded with valid credentials into the records — until 7 Nov 2026
  it accepted any connection from anywhere without authentication. If zigbee2mqtt runs on another
  machine: set `mqtt.bind` to the box's address and an account in `mqtt.local_users` (in z2m
  `mqtt.user`/`mqtt.password`); without an account the proxy then refuses to start. Loopback
  connections still need no login.
- **Web UI** (`:8097`): since 1.2.0 the **statistics** are visible without login
  (`server.public_stats`, `server.public_devices`; device names included) and listen on
  `server.host`. **Protected** are the live view (`/mqtt`, every MQTT message), `/config` and
  `/filters`: with `server.password` after signing in (page or basic auth), without a password for
  requests from `127.0.0.1` only — **so behind a reverse proxy on the same box always set a
  password**. The live stream is only served to the same origin (protection against foreign web
  pages in the browser). The login is unencrypted over the LAN; outside a trusted network use an
  encrypted access (VPN, SSH tunnel). Failed attempts are slowed to about one per second.
- **An upstream broker in front** (e.g. mosquitto with `mqttbridge --bidi`) needs the same care:
  whatever reaches it reaches the proxy through the bridge.
- **Return path:** whoever can write `cmd/zb/…/set` in this proxy's branch at the upstream broker
  switches the controls of all devices — the broker has to restrict that to your own backend
  ([backend_en.md › Securing](backend_en.md#what-your-broker-has-to-secure)).
  Restrict to single devices: `control.allow: list` with `control.devices`; switch off entirely:
  `control.enabled: false`. Settings and calibration only with `allow: writable`.
- **Encrypt the connection to the upstream** as soon as it leaves the premises (`ssl://`/`wss://`
  or `https://`).
- The proxy is checked regularly with vet, tests, known vulnerabilities (govulncheck), semgrep
  and a scan for credentials in the repository.
