package main

import (
	"context"
	"log"
	"time"

	"dncs/backend/internal/config"
	"dncs/backend/internal/database"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	if err := database.Migrate(ctx, pool, cfg.MigrationsDir); err != nil {
		log.Fatal(err)
	}
	log.Println("database migrations are up to date")
}
