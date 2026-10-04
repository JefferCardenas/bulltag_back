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
)

func main() {
	if err := run(); err != nil {
		slog.Error("el servicio terminó con error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg, err := LoadConfig()
	if err != nil {
		return err
	}

	// ctx se cancela cuando recibimos Ctrl+C o una señal de parada de Docker.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := NewStore(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer store.Close() // "defer" ejecuta esto al salir de la función

	h := &Handler{store: store, token: cfg.APIToken, log: logger}

	// Desde Go 1.22 el router estándar acepta "MÉTODO /ruta".
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", h.Health)
	mux.HandleFunc("POST /v1/ingest/chirpstack", h.ChirpStackUplink)

	// API de lectura (la usa el mapa).
	mux.HandleFunc("GET /v1/positions/live", h.LivePositions)
	mux.HandleFunc("GET /v1/animals", h.ListAnimals)
	mux.HandleFunc("GET /v1/animals/{id}/track", h.Track)

	// Página del mapa: todo lo que no coincida con las rutas anteriores.
	web, err := webHandler()
	if err != nil {
		return err
	}
	mux.Handle("GET /", web)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}

	// Cuando llegue la señal de parada, cerramos el servidor dejando
	// terminar las peticiones en curso (hasta 10 segundos).
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	logger.Info("ingest-service escuchando", "port", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	logger.Info("ingest-service detenido")
	return nil
}
