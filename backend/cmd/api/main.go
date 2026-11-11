// Command api runs the geofencing REST API and WebSocket alert server.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rebel600/geofencing-realtime-alert-system/backend/internal/config"
	"github.com/rebel600/geofencing-realtime-alert-system/backend/internal/httpapi"
	"github.com/rebel600/geofencing-realtime-alert-system/backend/internal/store"
	"github.com/rebel600/geofencing-realtime-alert-system/backend/internal/ws"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	log.Info("database ready, migrations applied")

	hub := ws.NewHub(log, httpapi.CheckOrigin(cfg.AllowedOrigins))
	hubStop := make(chan struct{})
	go hub.Run(hubStop)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           httpapi.New(cfg, st, hub, log),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", srv.Addr, "auth", cfg.APIKey != "", "origins", cfg.AllowedOrigins)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	close(hubStop) // closes WebSocket clients; hijacked conns are not tracked by Shutdown
	return srv.Shutdown(shutdownCtx)
}
