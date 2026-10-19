package main

import (
	"context"
	"example.com/marketplace/internal/platform/config"
	"example.com/marketplace/services/search"
	"flag"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"time"
)

func main() {
	id := flag.String("projection", uuid.NewString(), "new projection UUID; activated after successful catch-up")
	flag.Parse()
	c, e := config.Load("search")
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	db, e := pgxpool.New(ctx, c.DatabaseURL)
	if e != nil {
		panic(e)
	}
	defer db.Close()
	if e = search.Reindex(ctx, db, c, *id); e != nil {
		fmt.Fprintln(os.Stderr, "rebuild failed; previous projection preserved:", e)
		os.Exit(1)
	}
	fmt.Println("Active projection:", *id)
}
