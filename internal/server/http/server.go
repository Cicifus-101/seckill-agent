package http

import (
	"fmt"
	"net/http"
	"seckill-agent/internal/config"
	"time"
)

func NewServer(cfg config.HTTPConfig, handler *Handler) *http.Server {
	return &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:           handler.Routes(),
		ReadTimeout:       time.Duration(cfg.ReadTimeoutSeconds) * time.Second,
		WriteTimeout:      time.Duration(cfg.WriteTimeoutSeconds) * time.Second,
		IdleTimeout:       time.Duration(cfg.IdleTimeoutSeconds) * time.Second,
		ReadHeaderTimeout: 3 * time.Second,
	}
}
