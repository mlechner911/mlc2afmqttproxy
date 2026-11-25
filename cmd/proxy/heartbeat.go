package main

import (
	"time"

	mqtt "github.com/mochi-mqtt/server/v2"
	"mlc2afmqttproxy/pkg/broker"
	"mlc2afmqttproxy/pkg/forwarder"
	"mlc2afmqttproxy/pkg/metrics"
	"mlc2afmqttproxy/pkg/storage"
	"mlc2afmqttproxy/pkg/worker"
)

// heartbeatReadings liefert die Zusatzwerte des Heartbeats an den Sensor Monitor
// (T-AFMQ-20261125-01): Ein Proxy kann „läuft“ melden und trotzdem nichts weiterleiten —
// diese Werte zeigen es. Alles aus Zählern und Merkern, kein Scan des Puffers.
//
// Alter in Sekunden ist -1, solange es das Ereignis seit dem Start noch nicht gab.
func heartbeatReadings(db *storage.Store, fwd forwarder.Forwarder, w *worker.Worker, local *mqtt.Server) func() map[string]float64 {
	return func() map[string]float64 {
		st := metrics.GetStats()
		pending, _ := db.GetSize()
		return map[string]float64{
			"buffer_pending":       float64(pending),
			"buffer_pending_mb":    float64(metrics.PendingMB()),
			"upstream_connected":   boolValue(upstreamUp(fwd, w)),
			"zigbee_connected":     boolValue(broker.LocalClients(local) > 0),
			"last_forward_age_s":   ageSeconds(w.LastForward()),
			"last_receive_age_s":   ageSeconds(metrics.LastReceived()),
			"forward_failed_total": float64(st.MessagesForwardFailed),
			"dropped_full_total":   float64(st.MessagesDroppedFull),
			"dead_lettered_total":  float64(st.MessagesDeadLettered),
		}
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
