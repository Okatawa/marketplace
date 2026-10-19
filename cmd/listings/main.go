package main

import (
	"context"
	"example.com/marketplace/internal/platform/app"
	"example.com/marketplace/internal/platform/config"
	"example.com/marketplace/services/listings"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"time"
)

func main() {
	app.Main("listings", func(ctx context.Context, db *pgxpool.Pool, c config.Config) (http.Handler, []app.Worker, error) {
		if e := listings.NewMedia(c).Ensure(ctx); e != nil {
			return nil, nil, e
		}
		return listings.New(db, c), []app.Worker{app.Outbox(db, c, "marketplace.listings.v1"), {Name: "cleanup", Run: func(ctx context.Context) error {
			if e := listings.Cleanup(ctx, db, c); e != nil {
				return e
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Minute):
				return nil
			}
		}}}, nil
	})
}
