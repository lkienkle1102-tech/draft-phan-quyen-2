package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"example.com/phan-quyen-golang/internal/shared/app"
	"example.com/phan-quyen-golang/internal/shared/config"
)

func main() {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx, cfg); err != nil {
		log.Fatal(err)
	}
}
