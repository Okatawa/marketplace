package main

import (
	"example.com/marketplace/internal/platform/config"
	"example.com/marketplace/services/accounts"
	"example.com/marketplace/services/listings"
	"example.com/marketplace/services/messaging"
	"example.com/marketplace/services/notifications"
	"example.com/marketplace/services/search"
	"flag"
	"fmt"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"io/fs"
	"net/url"
	"os"
	"strings"
)

func main() {
	service := flag.String("service", "all", "service name or all")
	flag.Parse()
	files := map[string]fs.FS{"accounts": accounts.Migrations, "listings": listings.Migrations, "search": search.Migrations, "messaging": messaging.Migrations, "notifications": notifications.Migrations}
	if *service != "all" {
		if _, ok := files[*service]; !ok {
			fmt.Fprintln(os.Stderr, "unknown service")
			os.Exit(1)
		}
	}
	for _, name := range []string{"accounts", "listings", "search", "messaging", "notifications"} {
		if *service != "all" && *service != name {
			continue
		}
		source, e := iofs.New(files[name], "migrations")
		if e != nil {
			panic(e)
		}
		dsn := os.Getenv("DATABASE_URL")
		if dsn == "" {
			u := url.URL{Scheme: "pgx5", User: url.UserPassword(name, os.Getenv("APP_DB_PASSWORD")), Host: config.Env("POSTGRES_HOST", "postgres:5432"), Path: name, RawQuery: "sslmode=" + config.Env("POSTGRES_SSLMODE", "disable")}
			dsn = u.String()
		} else {
			dsn = strings.Replace(dsn, "postgres://", "pgx5://", 1)
			dsn = strings.Replace(dsn, "postgresql://", "pgx5://", 1)
		}
		m, e := migrate.NewWithSourceInstance("iofs", source, dsn)
		if e == nil {
			e = m.Up()
		}
		if e != nil && e != migrate.ErrNoChange {
			fmt.Fprintln(os.Stderr, "migration failed for", name, "(check database connectivity and migration state)")
			os.Exit(1)
		}
		m.Close()
		fmt.Println(name, "migrated")
	}
}
