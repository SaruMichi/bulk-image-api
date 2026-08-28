package main

import (
	"context"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SaruMichi/bulk-image-api/internal/config"
	"github.com/SaruMichi/bulk-image-api/internal/handler"
	"github.com/SaruMichi/bulk-image-api/internal/repository"
	"github.com/SaruMichi/bulk-image-api/internal/service"
)

func main() {
	cfg := config.Load()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("gagal konek database: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("gagal ping database: %v", err)
	}

	uploadDir := filepath.Join(cfg.StorageDir, "original")
	resultDir := filepath.Join(cfg.StorageDir, "result")

	jobRepo := repository.NewJobRepository(pool)
	jobService := service.NewJobService(jobRepo, resultDir, cfg.MaxImageWidth, cfg.WatermarkPath)
	jobHandler := handler.NewJobHandler(jobService, uploadDir)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	jobHandler.Routes(r)

	log.Printf("server jalan di :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, r); err != nil {
		log.Fatalf("server berhenti: %v", err)
	}
}
