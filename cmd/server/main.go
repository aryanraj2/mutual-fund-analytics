// cmd/server/main.go
package main

import (
	"context"
	"log"
	"net/http"
	"mutual-fund-analytics/internal/analytics"
	"mutual-fund-analytics/internal/api"
	"mutual-fund-analytics/internal/config"
	"mutual-fund-analytics/internal/mfapi"
	"mutual-fund-analytics/internal/pipeline"
	"mutual-fund-analytics/internal/ratelimiter"
	"mutual-fund-analytics/internal/store"

	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file, reading from environment")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	db, err := store.New(store.Config{
		Host:     cfg.DBHost,
		Port:     cfg.DBPort,
		User:     cfg.DBUser,
		Password: cfg.DBPassword,
		DBName:   cfg.DBName,
		SSLMode:  cfg.DBSSLMode,
	})
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer db.Close()

	limiter := ratelimiter.New(db.Conn)
	client  := mfapi.NewClient()
	orch    := pipeline.NewOrchestrator(db, client, limiter)
	engine  := analytics.NewEngine(db)

	ctx := context.Background()

	// Step 1 — Backfill
	log.Println("🚀 Starting backfill...")
	if err := orch.Backfill(ctx); err != nil {
		log.Fatalf("backfill: %v", err)
	}

	// Step 2 — Compute analytics
	log.Println("📊 Computing analytics...")
	if err := engine.ComputeAll(ctx); err != nil {
		log.Fatalf("analytics: %v", err)
	}

	// Step 3 — Start daily sync in background
	go orch.StartDailySync(ctx)

	// Step 4 — Start HTTP server
	handler := api.NewHandler(db, engine, orch)
	router  := api.NewRouter(handler)

	log.Printf("🌐 Server listening on :%s", cfg.ServerPort)
	if err := http.ListenAndServe(":"+cfg.ServerPort, router); err != nil {
		log.Fatalf("server: %v", err)
	}
}