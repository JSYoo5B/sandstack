package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/JSYoo5B/sandstack/internal/api/server"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		slog.Error("SandStack failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	handler, err := server.New(server.Config{
		Username:    os.Getenv("SANDSTACK_USERNAME"),
		Password:    os.Getenv("SANDSTACK_PASSWORD"),
		ProjectName: os.Getenv("SANDSTACK_PROJECT"),
		AdminToken:  os.Getenv("SANDSTACK_ADMIN_TOKEN"),
		PublicURL:   os.Getenv("SANDSTACK_PUBLIC_URL"),
	})
	if err != nil {
		return err
	}

	address := os.Getenv("SANDSTACK_LISTEN_ADDR")
	if address == "" {
		address = "127.0.0.1:8080"
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}

	httpServer := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	stopped := make(chan error, 1)
	go func() { stopped <- httpServer.Serve(listener) }()
	slog.Info("SandStack listening", "address", listener.Addr().String())

	select {
	case err := <-stopped:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdown)
	}
}
