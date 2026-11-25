package main

import (
	"time"

	mqtt "github.com/mochi-mqtt/server/v2"
	"mlc2afmqttproxy/pkg/broker"
	"mlc2afmqttproxy/pkg/config"
	"mlc2afmqttproxy/pkg/forwarder"
	"mlc2afmqttproxy/pkg/metrics"
	"mlc2afmqttproxy/pkg/storage"
	"mlc2afmqttproxy/pkg/web"
	"mlc2afmqttproxy/pkg/worker"
)

// heartbeatReadings liefert die Zusatzwerte des Heartbeats an den Sensor Monitor
// (T-AFMQ-20261125-01): Ein Proxy kann „läuft“ melden und trotzdem nichts weiterleiten —
// diese Werte zeigen es. Alles aus Zählern und Merkern, kein Scan des Puffers.
//
// Die Schlüssel heißen so, wie mlcmonit sie angemeldet hat (svc_…, Migration 00292 dort);
// einen unbekannten Schlüssel verwirft der Ingest still. Ein Alter fehlt, solange es das
// Ereignis seit dem Start noch nicht gab (statt -1, das als Wert missverstanden würde).
func heartbeatReadings(db *storage.Store, fwd forwarder.Forwarder, w *worker.Worker, local *mqtt.Server) func() map[string]float64 {
	return func() map[string]float64 {
		st := metrics.GetStats()
		pending, _ := db.GetSize()
		out := map[string]float64{
			"svc_buffer_pending":       float64(pending),
			"svc_buffer_mb":            float64(metrics.PendingMB()),
			"svc_upstream_connected":   boolValue(upstreamUp(fwd, w)),
			"svc_zigbee_connected":     boolValue(broker.LocalClients(local) > 0),
			"svc_forward_failed_total": float64(st.MessagesForwardFailed),
			"svc_dropped_full_total":   float64(st.MessagesDroppedFull),
			"svc_dead_lettered_total":  float64(st.MessagesDeadLettered),
		}
		if t := w.LastForward(); !t.IsZero() {
			out["svc_last_forward_age_s"] = ageSeconds(t)
		}
		if t := metrics.LastReceived(); !t.IsZero() {
			out["svc_last_receive_age_s"] = ageSeconds(t)
		}
		return out
	}
}

func boolValue(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func ageSeconds(t time.Time) float64 {
	if t.IsZero() {
		return -1
	}
	return float64(int64(time.Since(t).Seconds()))
}

// statusFunc liefert den Kurzstatus für /api/v1/status — dieselben Quellen wie der Heartbeat.
func statusFunc(cfg *config.Config, db *storage.Store, fwd forwarder.Forwarder, w *worker.Worker,
	local **mqtt.Server, paused *func() bool, start time.Time) func() web.Status {
	return func() web.Status {
		pending, _ := db.GetSize()
		return web.Status{
			Version:           Version,
			UptimeS:           int64(time.Since(start).Seconds()),
			Mode:              cfg.Mode,
			State:             forwardState(fwd, w, *paused),
			BufferPending:     int64(pending),
			UpstreamConnected: upstreamUp(fwd, w),
			ZigbeeConnected:   broker.LocalClients(*local) > 0,
			LastForwardAgeS:   int64(ageSeconds(w.LastForward())),
			LastReceiveAgeS:   int64(ageSeconds(metrics.LastReceived())),
		}
	}
}
