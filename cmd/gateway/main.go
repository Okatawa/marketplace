package main

import (
	"context"
	"example.com/marketplace/internal/platform/app"
	"example.com/marketplace/internal/platform/config"
	"example.com/marketplace/services/gateway"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
)

func main() {
	app.Main("gateway", func(ctx context.Context, db *pgxpool.Pool, c config.Config) (http.Handler, []app.Worker, error) {
		return gateway.New(c), nil, nil
	})
}
