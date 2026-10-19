// Package app owns process lifecycle, never service business logic.
package app

import (
	"context"
	"example.com/marketplace/internal/platform/config"
	"example.com/marketplace/internal/platform/httpx"
	"example.com/marketplace/internal/platform/kafka"
	"example.com/marketplace/internal/platform/outbox"
	"example.com/marketplace/internal/platform/telemetry"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type Worker struct {
	Name string
	Run  func(context.Context) error
}
type Factory func(context.Context, *pgxpool.Pool, config.Config) (http.Handler, []Worker, error)

func Main(service string, factory Factory) {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", service))
	if e := run(service, factory); e != nil {
		slog.Error("service stopped", "error_type", "startup_or_runtime")
		os.Exit(1)
	}
}
func run(service string, factory Factory) error {
	cfg, e := config.Load(service)
	if e != nil {
		return e
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	observability, e := telemetry.Init(ctx, service)
	if e != nil {
		return e
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		observability.Shutdown(c)
	}()
	var db *pgxpool.Pool
	if service != "gateway" {
		pc, e := pgxpool.ParseConfig(cfg.DatabaseURL)
		if e != nil {
			return e
		}
		pc.ConnConfig.Tracer = telemetry.SQLTracer{}
		pc.MaxConns = 10
		db, e = pgxpool.NewWithConfig(ctx, pc)
		if e != nil {
			return e
		}
		defer db.Close()
		if e = db.Ping(ctx); e != nil {
			return e
		}
	}
	handler, tasks, e := factory(ctx, db, cfg)
	if e != nil {
		return e
	}
	workers, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	for _, task := range tasks {
		wg.Add(1)
		go func(task Worker) {
			defer wg.Done()
			for workers.Err() == nil {
				if e := task.Run(workers); e != nil && workers.Err() == nil {
					slog.Warn("worker restarting", "worker", task.Name, "error_type", "dependency")
				}
				if !kafka.Pause(workers, time.Second) {
					return
				}
			}
		}(task)
	}
	if db != nil {
		p, e := kafka.NewProducer(cfg.KafkaBrokers)
		if e != nil {
			cancel()
			wg.Wait()
			return e
		}
		defer p.Close()
		wg.Add(1)
		go func() { defer wg.Done(); observability.Backlog(workers, db, service, p.Client) }()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if e := httpx.Run(workers, config.Env("METRICS_ADDR", ":9090"), observability.Metrics()); e != nil && workers.Err() == nil {
			slog.Error("metrics server stopped")
			stop()
		}
	}()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) { httpx.JSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		if db != nil {
			c, cancel := context.WithTimeout(r.Context(), time.Second)
			defer cancel()
			if e := db.Ping(c); e != nil {
				httpx.JSON(w, 503, map[string]string{"status": "unavailable"})
				return
			}
		}
		httpx.JSON(w, 200, map[string]string{"status": "ready"})
	})
	mux.Handle("/", handler)
	slog.Info("listening", "address", cfg.Addr)
	e = httpx.Run(ctx, cfg.Addr, telemetry.WrapHTTP(mux, slog.Default(), observability.Registry))
	cancel()
	wg.Wait()
	return e
}
func Outbox(db *pgxpool.Pool, cfg config.Config, topic string) Worker {
	return Worker{"outbox", func(ctx context.Context) error {
		p, e := kafka.NewProducer(cfg.KafkaBrokers)
		if e != nil {
			return e
		}
		defer p.Close()
		return outbox.Run(ctx, db, p, topic)
	}}
}
func Consume(cfg config.Config, topic, group string, handle func(context.Context, kafka.Envelope) error) Worker {
	return Worker{"consumer", func(ctx context.Context) error {
		c, e := kafka.NewConsumer(cfg.KafkaBrokers, topic, group)
		if e != nil {
			return e
		}
		defer c.Close()
		return c.Run(ctx, handle)
	}}
}
