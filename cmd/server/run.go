package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/go-chi/chi/v5"
	"github.com/magnm/spale/config"
	"github.com/magnm/spale/pkg/webhook"
)

// Run starts a chi http server
func Run() {
	cfg := config.Config{}
	if err := env.Parse(&cfg); err != nil {
		slog.Error("failed to parse config", "err", err)
		os.Exit(1)
	}
	config.Current = cfg

	setupLogging(cfg)

	router := chi.NewRouter()
	router.Post("/mutate", webhook.HandleRequest)

	errChan := make(chan error, 1)
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.Port),
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// Start the http server
	go func() {
		slog.Info("http server listening", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil {
			slog.Info("server error", "err", err)
			errChan <- err
		}
	}()

	// Start https server if cert and key are provided
	tlsCert := "/etc/ssl/cert.pem"
	tlsKey := "/etc/ssl/cert.key"
	if cfg.TlsCert != "" && cfg.TlsKey != "" {
		tlsCert = cfg.TlsCert
		tlsKey = cfg.TlsKey
	}

	tlsSrv := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.TlsPort),
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	go func() {
		slog.Info("https server listening", "port", cfg.TlsPort)
		if err := tlsSrv.ListenAndServeTLS(tlsCert, tlsKey); err != nil {
			slog.Info("server error", "err", err)
			errChan <- err
		}
	}()

	select {
	case err := <-errChan:
		slog.Error("server error", "err", err)
		os.Exit(1)
	case <-stopChan:
		slog.Info("server stopping")
	}

	// Give the server 5 seconds to shutdown gracefully
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}
