package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"emotion-garden/internal/config"
	"emotion-garden/internal/storage"
)

func main() {
	cmd := "up"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	bundle, err := config.LoadFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config load error: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := storage.NewPool(ctx, bundle.Env.DatabaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "db connect error: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	switch cmd {
	case "up":
		if err := storage.MigrateUp(ctx, pool, "./migrations"); err != nil {
			fmt.Fprintf(os.Stderr, "migrate up failed: %v\n", err)
			os.Exit(1)
		}
		userID, err := storage.EnsureBootstrap(
			ctx,
			pool,
			bundle.Env.DefaultTimezone,
			bundle.Env.GardenDefaultCols,
			bundle.Env.GardenDefaultRows,
			bundle.Models.DefaultReleaseMode,
			bundle.Models.DefaultGrowMode,
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bootstrap seed failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("migrations applied; bootstrap_user_id=%s\n", userID.String())

	case "verify":
		if err := storage.VerifySchemaAndOrphans(ctx, pool); err != nil {
			fmt.Fprintf(os.Stderr, "schema verification failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("schema and relational integrity verified: OK")

	default:
		fmt.Fprintf(os.Stderr, "unknown command %q (expected 'up' or 'verify')\n", cmd)
		os.Exit(1)
	}
}
