package main

import (
	"context"
	"example.com/marketplace/internal/platform/app"
	"example.com/marketplace/internal/platform/config"
	"example.com/marketplace/services/notifications"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
)

func main() {
	app.Main("notifications", func(ctx context.Context, db *pgxpool.Pool, c config.Config) (http.Handler, []app.Worker, error) {
		return http.NewServeMux(), []app.Worker{app.Consume(c, "marketplace.messages.v1", "notifications-v1", (notifications.Inbox{DB: db}).Accept), {Name: "email", Run: (notifications.Worker{DB: db, Config: c, Sender: notifications.SMTP{Addr: c.SMTPAddr, From: c.SMTPFrom, Production: c.Production}}).Run}}, nil
	})
}
