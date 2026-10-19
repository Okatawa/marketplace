package main

import (
	"context"
	"example.com/marketplace/internal/platform/app"
	"example.com/marketplace/internal/platform/config"
	"example.com/marketplace/services/search"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
)

func main() {
	app.Main("search", func(ctx context.Context, db *pgxpool.Pool, c config.Config) (http.Handler, []app.Worker, error) {
		return search.New(db, c), []app.Worker{{Name: "projection", Run: func(ctx context.Context) error { return search.Run(ctx, db, c) }}}, nil
	})
}
