package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"seckill-agent/internal/bootstrap"
	"syscall"
)

func main() {
	// 返回空的、顶级的Context
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	app, err := bootstrap.NewApp()
	if err != nil {
		log.Fatalf("bootstrap application failed: %v", err)
	}

	if err = app.Run(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		app.Logger().ErrorContext(ctx, "application stopped with error", "error", err)
		log.Fatalf("run application failed: %v", err)
	}
}
