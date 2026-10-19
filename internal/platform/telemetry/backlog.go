package telemetry

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"strconv"
	"time"
)

func (t *Runtime) Backlog(ctx context.Context, db *pgxpool.Pool, service string, client *kgo.Client) {
	pending := prometheus.NewGauge(prometheus.GaugeOpts{Name: "marketplace_outbox_pending", Help: "Unpublished outbox records"})
	age := prometheus.NewGauge(prometheus.GaugeOpts{Name: "marketplace_outbox_oldest_seconds", Help: "Age of oldest unpublished event"})
	jobs := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "marketplace_notification_jobs", Help: "Notification jobs by state"}, []string{"state"})
	lag := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "marketplace_consumer_lag", Help: "Consumer group lag"}, []string{"group", "topic", "partition"})
	t.Registry.MustRegister(pending, age, jobs, lag)
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		work, cancel := context.WithTimeout(ctx, 5*time.Second)
		if service == "listings" || service == "messaging" {
			var count, seconds float64
			if e := db.QueryRow(work, "SELECT count(*),coalesce(extract(epoch FROM now()-min(created_at)),0) FROM outbox WHERE sent_at IS NULL").Scan(&count, &seconds); e == nil {
				pending.Set(count)
				age.Set(seconds)
			}
		}
		if service == "notifications" {
			rows, e := db.Query(work, "SELECT state,count(*) FROM jobs GROUP BY state")
			if e == nil {
				jobs.Reset()
				for rows.Next() {
					var state string
					var n float64
					if rows.Scan(&state, &n) == nil {
						jobs.WithLabelValues(state).Set(n)
					}
				}
				rows.Close()
			}
		}
		if client != nil && (service == "search" || service == "notifications") {
			group := "notifications-v1"
			if service == "search" {
				_ = db.QueryRow(work, "SELECT group_id FROM projections p JOIN settings s ON p.id=s.active_projection").Scan(&group)
			}
			groups, e := kadm.NewClient(client).Lag(work, group)
			if e == nil {
				lag.Reset()
				for _, g := range groups {
					for topic, partitions := range g.Lag {
						for partition, l := range partitions {
							lag.WithLabelValues(group, topic, strconv.Itoa(int(partition))).Set(float64(l.Lag))
						}
					}
				}
			}
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
