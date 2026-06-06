# HTTP-Ingest-Endpunkt

Endpunkt zum Einliefern von Messwerten per HTTP — für **eigene Hardware (ESP32)**
und **Fremdsysteme**, die nicht über MQTT anbinden. Schreibt über denselben
Ingest-Kern (Sink) wie die MQTT-Quelle: Auto-Discovery von Gerät/Proben, Speicherung
mit Kalibrier-Offset, idempotent.

## Übersicht

| | |
| :--- | :--- |
| **Methode / Pfad** | `POST /api/v1/ingest` |
| **Content-Type** | `application/json` |
| **Go-API direkt (Dev)** | `http://<server-domain>:58080/api/v1/ingest` |
| **Über Caddy (Prod, Single-Entry)** | `https://<server-domain>:58443/api/v1/ingest` |
| **Backend-Bind** | `HTTP_ADDR` (Default `0.0.0.0:58080`; Container i.d.R. `:58080`) |
| **Auth** | optionaler Geräte-Token (s. u.) — **kein** `mlcauth`/JWT |

## Authentifizierung

Der Endpunkt nutzt **nicht** die `forward_auth`/JWT-Kette von `mlcauth` (Geräte haben
keine User-Session), sondern einen optionalen **statischen Token**:

- Server-seitig via Umgebungsvariable **`INGEST_TOKEN`** gesetzt.
- Client sendet ihn im Header **`X-Ingest-Token: <token>`**.
- Ist `INGEST_TOKEN` **leer** (Default), ist der Endpunkt **offen** (nur für
  vertrauenswürdiges internes Netz gedacht).
- Falsch/fehlend bei gesetztem Token → `401` (`ERR_UNAUTHENTICATED`).

> Im Forward-Proxy daher **`/api/v1/ingest` von `forward_auth` ausnehmen** und den
> Geräte-Token verwenden (siehe Caddy-Beispiel).

## Request-Body

```json
{
  "device": "freezer-lab-1",
  "ieee": "0x00124b00257f1a2b",
  "readings": [
    { "key": "temperature", "value": 4.21, "ts": "2026-06-06T12:00:00Z" },
    { "key": "humidity", "value": 55.3 }
  ]
}
```

| Feld | Typ | Pflicht | Beschreibung |
| :--- | :--- | :--- | :--- |
| `device` | string | ja | Stabiler Geräte-Schlüssel. Wird zur Identität/zum Anlegen genutzt (als `mqtt_topic`/`friendly_name`). |
| `ieee` | string | nein | Stabile Hardware-ID (MAC/IEEE). Wenn vorhanden, primärer Identitäts-Schlüssel (überlebt Umbenennung). |
| `readings[]` | array | ja | Eine oder mehrere Messungen. |
| `readings[].key` | string | ja | Messgröße/Payload-Key (z. B. `temperature`, `humidity`, `voltage`, `co2`). Unbekannte Keys werden übersprungen. |
| `readings[].value` | number | ja | Messwert. Binär als `0`/`1`. |
| `readings[].ts` | string (RFC3339) | nein | Zeitpunkt. Fehlt er → jetzt. Für Nachlieferung/Catch-Up den echten Zeitpunkt senden. |

## Response

`200 OK`:

```json
{ "stored": 2, "skipped": 0 }
```

- `stored` = geschriebene Messwerte, `skipped` = übersprungene (unbekannter Key,
  ungültiges `ts`, oder Probe ohne `store`-Flag).

**Fehler** (einheitlicher Envelope `{ "error": { "code", "message" } }`):

| Status | Code | Ursache |
| :--- | :--- | :--- |
| 400 | `ERR_VALIDATION` | Body ungültig / `readings` leer |
| 401 | `ERR_UNAUTHENTICATED` | Token erforderlich, fehlt/falsch |
| 500 | `ERR_INTERNAL` | Serverfehler |

## Verhalten / Hinweise

- **Auto-Discovery:** Unbekanntes `device` wird angelegt (`connection_type = ESP32`),
  unbekannte `key`s werden als Proben angelegt.
- **`store`-Flag:** Langzeit-Speicherung in `measurements` nur, wenn die Probe
  `store = true` hat (wie bei MQTT). `probe_state` (Live-Wert) wird immer
  aktualisiert. Neue Proben starten mit `store = false` → ggf. in der Sensoren-UI
  aktivieren.
- **Idempotenz:** Punkte sind eindeutig pro `(probe_id, time)` — derselbe Messwert
  mit gleichem `ts` mehrfach gesendet landet **nur einmal** (sicher für Retries/
  Store-and-Forward).
- **Kalibrier-Offset:** wird beim Schreiben auf analoge Werte angewandt (binär roh).
- **`probe_state`** wird bei expliziten Zeitpunkten nur aktualisiert, wenn der Punkt
  **nicht älter** als der bekannte Live-Wert ist (Catch-Up dreht den Status nicht
  zurück).

## Beispiele (curl)

Offen (kein Token gesetzt):

```bash
curl -X POST http://<server-domain>:58080/api/v1/ingest \
  -H 'Content-Type: application/json' \
  -d '{"device":"freezer-lab-1","ieee":"0x00124b00257f1a2b",
       "readings":[{"key":"temperature","value":4.21}]}'
```

Mit Token:

```bash
curl -X POST https://<server-domain>:58443/api/v1/ingest \
  -H 'Content-Type: application/json' \
  -H 'X-Ingest-Token: <DEIN_TOKEN>' \
  -d '{"device":"freezer-lab-1","readings":[{"key":"temperature","value":4.21,"ts":"2026-06-06T12:00:00Z"}]}'
```

## OpenAPI (3.0) — Fragment

```yaml
openapi: 3.0.3
info:
  title: MLC Sensor Monitor — Ingest
  version: "1.0"
servers:
  - url: https://<server-domain>:58443
    description: Caddy (Single-Entry, Prod)
  - url: http://<server-domain>:58080
    description: Go-API direkt (Dev)
paths:
  /api/v1/ingest:
    post:
      summary: Messwerte einliefern (HTTP)
      security:
        - ingestToken: []   # nur relevant, wenn INGEST_TOKEN gesetzt ist
      requestBody:
        required: true
        content:
          application/json:
            schema: { $ref: '#/components/schemas/IngestRequest' }
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema: { $ref: '#/components/schemas/IngestResponse' }
        "400": { description: Validierungsfehler }
        "401": { description: Token fehlt/falsch }
components:
  securitySchemes:
    ingestToken:
      type: apiKey
      in: header
      name: X-Ingest-Token
  schemas:
    IngestRequest:
      type: object
      required: [device, readings]
      properties:
        device:   { type: string, example: "freezer-lab-1" }
        ieee:     { type: string, example: "0x00124b00257f1a2b" }
        readings:
          type: array
          minItems: 1
          items:
            type: object
            required: [key, value]
            properties:
              key:   { type: string, example: "temperature" }
              value: { type: number, example: 4.21 }
              ts:    { type: string, format: date-time, example: "2026-06-06T12:00:00Z" }
    IngestResponse:
      type: object
      properties:
        stored:  { type: integer, example: 2 }
        skipped: { type: integer, example: 0 }
```

## Caddy — Einbau in den Forward-Proxy

Den Ingest-Pfad **vor** der `forward_auth`-Regel behandeln, damit Geräte ohne JWT
durchkommen (sie authentisieren per `X-Ingest-Token`):

```caddyfile
<server-domain> {
    # 1) Geräte-Ingest: KEIN forward_auth, nur Geräte-Token (im Backend geprüft)
    handle /api/v1/ingest {
        reverse_proxy mlc-sensor-backend:58080
    }

    # 2) Übrige API: menschliche Nutzer via mlcauth (forward_auth)
    handle /api/v1/* {
        forward_auth mlcauth:9099 {
            uri /verify
            copy_headers X-User-ID X-User-Email
        }
        reverse_proxy mlc-sensor-backend:58080
    }

    # 3) Frontend etc.
    handle { reverse_proxy mlc-sensor-admin:3000 }
}
```

> Backend-Upstream (`mlc-sensor-backend:58080`) und der `mlcauth`-`forward_auth`-Block
> sind Platzhalter — an euer Compose-Netz/euren `mlcauth`-Verify-Pfad anpassen.
> Optional kann Caddy zusätzlich den Token prüfen/erzwingen, bevor er proxyt.
