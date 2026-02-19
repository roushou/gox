package main

import (
	"context"
	"log"

	"github.com/roushou/gox/internal/app"
	"github.com/roushou/gox/internal/platform/config"
)

func main() {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	application, err := app.New(cfg, nil, nil)
	if err != nil {
		log.Fatalf("create app: %v", err)
	}

	ctx, cancel := app.ContextWithShutdownSignal(context.Background())
	defer cancel()

	if err := application.Run(ctx); err != nil {
		log.Fatalf("run app: %v", err)
	}
}
