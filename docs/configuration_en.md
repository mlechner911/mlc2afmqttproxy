# Configuration (`config.yaml`)

[🇩🇪 Auf Deutsch lesen](configuration.md)

The proxy is completely controlled via a central `config.yaml` file. This file must be located in the same directory as the `proxy` binary.

## Configuration Parameters

```yaml
# The operation mode of the upstream.
# Allowed values: "http" or "mqtt"
mode: "http"

# Note on MQTT mode & timestamps:
# Since we provide offline data later with Store & Forward, the timestamp is important.
# The option mqtt.timestamp_mode controls the behavior:
# - "none": Standard MQTT v3.1.1, timestamp is ignored.
# - "json_inject": (Recommended) Unpacks the payload (if JSON), inserts the timestamp as a Unix timestamp (ms) and repacks it.
# - "v5_property": Sends the message via MQTT v5 and passes the timestamp (RFC3339) as a "User Property" header.

server:
  # The port for the diagnostic web dashboard and the health API
  port: 8097
  # (Optional) Path prefix for the diagnostic REST API (Default: "/api/v1")
  api_prefix: "/api/v1"

storage:
  # Path where BadgerDB stores its data (Store & Forward buffer)
  path: "./data"

mqtt:
  # The local port on which the embedded Mochi broker listens for Zigbee2MQTT
  local_port: 1884
  # Upstream settings (only relevant if mode: "mqtt")
  upstream: "tcp://cloud-broker.example.com:1883"
  username: "user"
  password: "password"
  # How timestamps are sent to the MQTT upstream (none, json_inject, v5_property)
  timestamp_mode: "json_inject"
  # Name of the injected timestamp field in the JSON (Default: "_ts").
  # It uses "inject if absent", meaning if this field already exists, it remains untouched!
  timestamp_field: "_ts"
  
  # Optional rewrite policy for the base topic (e.g., to route to a namespace)
  topic_rewrite:
    match_prefix: "zigbee2mqtt/"
    replace_with: "/v1/bridgedataxxx/"

http:
  # Upstream settings (only relevant if mode: "http")
  endpoint: "https://api.example.com/ingest"
  # Optional: Will be sent as "Authorization: Bearer <token>" header
  token: "my-secret-token"

worker:
  # (Optional) The interval for the background worker in milliseconds (Default: 100)
  interval_ms: 100
  # (Optional) Maximum number of messages to send in a single loop/batch (Default: 100)
  max_batch_size: 50
  # (Optional) Throttling delay in milliseconds between messages within a batch (Default: 0)
  batch_delay_ms: 10
  # (Optional) Minimum backoff time on failures or connection drops in seconds (Default: 1)
  retry_min_s: 1
  # (Optional) Maximum backoff time on failures or connection drops in seconds (Default: 60)
  retry_max_s: 60
```

## Architecture Diagram by Operation Mode

The `mode` setting determines which path the worker uses to transmit the data to the cloud. The local data flow (Zigbee2MQTT -> Mochi -> BadgerDB) always remains identical.

```mermaid
graph TD
    subgraph Local Gateway
        Z[Zigbee2MQTT] -- "Publish (QoS 1)" --> M(Local Broker\nPort 1884)
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
