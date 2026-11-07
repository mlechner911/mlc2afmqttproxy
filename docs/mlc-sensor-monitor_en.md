# Connecting to the MLC Sensor Monitor

[🇩🇪 Deutsch](mlc-sensor-monitor.md) · General operation: [betrieb_en.md](betrieb_en.md)

This page applies **only** when the upstream is the **MLC Sensor Monitor**
([mlcgo.eu](https://mlcgo.eu)). Other upstreams authenticate differently (see
[betrieb_en.md › Authenticating to the upstream](betrieb_en.md#authenticating-to-the-upstream)).

## The ingest token

The MLC Sensor Monitor accepts data with an **ingest token**:

- **MQTT** (the monitor's embedded broker, default port 51883, or `wss://…/mqtt`): the token is the
  **password**. The **user name** is only logged — use the customer number or the box name so the
  connection is recognisable in the log.
- **HTTP** (`POST /api/v1/ingest`): the token as header `X-Ingest-Token`.

**The token is the tenant boundary.** It decides which tenant (customer) the data goes to — not the
user name and not the topic. A token belongs to exactly one tenant.

### Where the token comes from

- In the tenant's **application**: **MQTT sources › Ingest tokens › "Create token"** (right "manage
  company master data"). The plain text is shown **exactly once** — put it into `config.yaml`
  right away. The monitor only stores a checksum.
- For customers, the operator sets it up (operator console › tenant).
- **"Revoke"** invalidates a token immediately.

### Where it goes in `config.yaml`

```yaml
mqtt:
  upstream: "tcp://127.0.0.1:51883"      # monitor on the same host; otherwise ssl://<host>:… or wss://
  username: "box-warehouse-1"            # only to recognise the connection in the log
  password: "<the tenant's ingest token>"
  timestamp_mode: "v5_property"          # send the receive time (backfill)

http:
  endpoint: "http://127.0.0.1:58080/api/v1/ingest"
  token: "<the same ingest token>"       # for the heartbeat — same tenant as mqtt.password
```

`http.token` **must** equal `mqtt.password`: the heartbeat (see below) goes over HTTP; with a
different token the service would end up in a different tenant than the data.

### Wrong or revoked token

- The monitor refuses the MQTT connection ("bad username or password"); the monitor's log records
  the refusal with client ID and address.
- The proxy **keeps buffering**, nothing is lost (see [betrieb_en.md](betrieb_en.md)).
- The heartbeat stops → after a short while the monitor reports **"No data"** for the service
  `svc-edgeproxy` — that is how an expired or revoked token shows up.
- With a valid token and a restart the buffer drains; values arrive with their real time.

### Changing the token

1. Create a new token (MQTT sources).
2. Put it into `config.yaml` at `mqtt.password` **and** `http.token` (for a route, its
   `password`). Back up the configuration ([betrieb_en.md › Backing up the configuration](betrieb_en.md#backing-up-the-configuration)).
3. `sudo systemctl restart mlc-edge-proxy`; verify: no refusal in the log, `stats` without errors,
   values and heartbeat arrive in the monitor.
4. **Only then** revoke the old token.

## Several tenants from one box (routes)

If one box serves devices of different customers, a **route** sends selected devices with **its own
token** to another tenant:

```yaml
routes:
  - name: demo                            # buffer under <storage.path>/routes/demo
    upstream: "tcp://127.0.0.1:51883"
    username: "box-warehouse-1-demo"
    password: "<the other tenant's ingest token>"
    topic_alias: true
    devices:                              # IEEE addresses (stay the same when a device is renamed in z2m)
      - "0xa4c1388e4c14c889"
```

- Each route has its **own buffer and worker** — an unreachable target does not hold up the others
  — and its own MQTT client ID.
- With `devices`, **only** these devices go out via the route, plus a device list trimmed to them
  (`bridge/devices`); other `bridge/*` messages never.
- At start the log shows: `[Route demo] aktiv: Modus mqtt, 6 Gerät(e)`.

## Heartbeat and remote control

On by default (`monitor.enabled`), can be switched off:

- **Heartbeat:** every 30 s (`monitor.interval_s`) to `http.endpoint` with `http.token`. The monitor
  creates the device **`svc-edgeproxy`** from it and shows it on the **Services** page. If the
  heartbeat stops (proxy down, network gone, token revoked), the monitor reports "No data".
- **Remote control** (**Services** page, right "control services", with a reason, logged):
  **Pause** (forwarding stops, data stays in the buffer), **Resume** (buffer drains with the
  original times), **Restart**, **Stop**. The proxy receives the commands via the upstream broker it
  already uses (`cmd/svc/edgeproxy`) and acknowledges them. When the proxy is down it cannot take
  commands — the system administrator starts it on the box.
  - Only the monitor itself sends commands; a device or another access cannot write there
    (monitor since 7 Nov 2026).
  - A pause ends on its own after `monitor.max_pause_minutes` (default 60) at the latest — a
    forgotten pause would otherwise fill the buffer until new values are lost.
  - A stored (retained) command is not executed: it would come back on every restart.
  - **Restart** exits with code 75 so that a unit with `Restart=on-failure` restarts as well;
    **Stop** exits with 0 — the box stays off.

## Checking that everything arrives

- Proxy: `curl http://127.0.0.1:8097/api/v1/stats` — buffer 0, no errors.
- Monitor: **Services** page — `svc-edgeproxy` running; **Devices** page — the Zigbee devices have
  fresh values.
- After an interruption: values from the pause appear in the history at their **measurement time**,
  not bunched at the moment of return.
