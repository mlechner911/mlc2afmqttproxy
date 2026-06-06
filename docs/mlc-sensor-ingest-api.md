# HTTP-Ingest-Endpunkt
 
Dieser Endpunkt beschreibt die HTTP-Schnittstelle des `mlcsensormonitor`, die dieser Proxy (im Modus `http`) beliefert.

Er dient zum Einliefern von Messwerten per HTTP — für **eigene Hardware (ESP32)** und **Fremdsysteme** (Zigbee2MQTT etc.), die nicht direkt über MQTT anbinden.

## Übersicht

| | |
| :--- | :--- |
| **Methode / Pfad** | `POST /api/v1/ingest` |
| **Content-Type** | `application/json` |
| **Auth** | HTTP-Header `X-Ingest-Token: <token>` |

## Authentifizierung

Zur Autorisierung muss im HTTP Request der Header **`X-Ingest-Token`** mitgeliefert werden. Ist der Token ungültig oder fehlt, wird der Request mit `401 Unauthorized` abgewiesen.

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
| `device` | string | ja | Stabiler Geräte-Schlüssel (identisch zum `mqtt_topic`/`friendly_name`). |
| `ieee` | string | nein | Stabile Hardware-ID (MAC/IEEE). |
| `readings[]` | array | ja | Eine oder mehrere Messungen. |
| `readings[].key` | string | ja | Messgröße (z. B. `temperature`, `humidity`). |
| `readings[].value` | number | ja | Messwert (Numerisch). |
| `readings[].ts` | string (RFC3339) | nein | Historischer Zeitpunkt. Fehlt dieser, wird die Empfangszeit serverseitig genutzt. |

## Response

Erfolgreich (`200 OK`):

```json
{ "stored": 2, "skipped": 0 }
```

Fehler:

| Status | Code | Ursache |
| :--- | :--- | :--- |
| 400 | `ERR_VALIDATION` | Body ungültig / `readings` leer |
| 401 | `ERR_UNAUTHENTICATED` | Token fehlt/falsch |
| 500 | `ERR_INTERNAL` | Serverfehler |

## Beispiel (curl)

```bash
curl -X POST https://<server-domain>/api/v1/ingest \
  -H 'Content-Type: application/json' \
  -H 'X-Ingest-Token: <DEIN_TOKEN>' \
  -d '{"device":"freezer-lab-1","readings":[{"key":"temperature","value":4.21,"ts":"2026-06-06T12:00:00Z"}]}'
```
