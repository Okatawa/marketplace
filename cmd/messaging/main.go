package main

import (
	"context"
	"example.com/marketplace/internal/platform/app"
	"example.com/marketplace/internal/platform/config"
	"example.com/marketplace/services/messaging"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
)

func main() {
	app.Main("messaging", func(ctx context.Context, db *pgxpool.Pool, c config.Config) (http.Handler, []app.Worker, error) {
		return messaging.New(db, c), []app.Worker{app.Outbox(db, c, "marketplace.messages.v1")}, nil
	})
}
