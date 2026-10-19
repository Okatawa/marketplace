package httpx

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"
)

func Run(ctx context.Context, addr string, handler http.Handler) error {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return Serve(ctx, l, handler)
}
func Serve(ctx context.Context, l net.Listener, handler http.Handler) error {
	s := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 60 * time.Second}
	done := make(chan error, 1)
	go func() { done <- s.Serve(l) }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := s.Shutdown(shutdown); err != nil {
			_ = s.Close()
			return err
		}
		return nil
	}
}
