package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr          string
	DatabasePath      string
	SessionTTL        time.Duration
	WorkerPoll        time.Duration
	WorkerLease       time.Duration
	ShutdownTimeout   time.Duration
	BootstrapAdmin    string
	BootstrapPassword string
	MaxBodyBytes      int64
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:          env("HTTP_ADDR", ":8080"),
		DatabasePath:      env("DATABASE_PATH", "youth-load.db"),
		BootstrapAdmin:    os.Getenv("BOOTSTRAP_ADMIN"),
		BootstrapPassword: os.Getenv("BOOTSTRAP_PASSWORD"),
		MaxBodyBytes:      1 << 20,
	}
	var err error
	if cfg.SessionTTL, err = duration("SESSION_TTL", 12*time.Hour); err != nil {
		return Config{}, err
	}
	if cfg.WorkerPoll, err = duration("WORKER_POLL_INTERVAL", 2*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.WorkerLease, err = duration("WORKER_LEASE", 30*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.ShutdownTimeout, err = duration("SHUTDOWN_TIMEOUT", 10*time.Second); err != nil {
		return Config{}, err
	}
	if raw := os.Getenv("MAX_BODY_BYTES"); raw != "" {
		cfg.MaxBodyBytes, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || cfg.MaxBodyBytes < 1024 {
			return Config{}, fmt.Errorf("MAX_BODY_BYTES: invalid positive size")
		}
	}
	return cfg, cfg.Validate()
}

func (c Config) Validate() error {
	if c.HTTPAddr == "" || c.DatabasePath == "" {
		return errors.New("http address and database path are required")
	}
	if c.SessionTTL <= 0 || c.WorkerPoll <= 0 || c.WorkerLease <= c.WorkerPoll {
		return errors.New("session and worker durations are invalid")
	}
	if c.ShutdownTimeout <= 0 || c.MaxBodyBytes <= 0 {
		return errors.New("shutdown timeout and body limit must be positive")
	}
	if (c.BootstrapAdmin == "") != (c.BootstrapPassword == "") {
		return errors.New("bootstrap admin and password must be provided together")
	}
	return nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func duration(key string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s: invalid duration", key)
	}
	return value, nil
}
