package search

import (
	"context"
	"example.com/marketplace/internal/platform/config"
	"example.com/marketplace/internal/platform/kafka"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

func Run(ctx context.Context, db *pgxpool.Pool, cfg config.Config) error {
	var id, group string
	if e := db.QueryRow(ctx, "SELECT p.id,p.group_id FROM settings s JOIN projections p ON p.id=s.active_projection").Scan(&id, &group); e != nil {
		return e
	}
	consumer, e := kafka.NewConsumer(cfg.KafkaBrokers, "marketplace.listings.v1", group)
	if e != nil {
		return e
	}
	defer consumer.Close()
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- consumer.Run(work, (Projection{DB: db, ID: id}).Apply) }()
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case e := <-done:
			return e
		case <-ctx.Done():
			cancel()
			return <-done
		case <-tick.C:
			var active string
			if e := db.QueryRow(ctx, "SELECT active_projection FROM settings").Scan(&active); e != nil {
				cancel()
				<-done
				return e
			}
			if active != id {
				cancel()
				<-done
				return nil
			}
		}
	}
}
