package config

import (
	"errors"
	"os"

	"github.com/joho/godotenv"
)

const (
	defaultDSN  = "postgres://auction:auction@localhost:5432/auction?sslmode=disable"
	defaultAddr = ":8080"
)

type Config struct {
	DatabaseURL string
	Listen      string
	JWTSecret   string
}

func Load() (*Config, error) {

	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	databaseURL := envOr("DATABASE_URL", defaultDSN)
	listen := envOr("HTTP_ADDR", defaultAddr)
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return nil, errors.New("JWT_SECRET is required")
	}
	return &Config{
		DatabaseURL: databaseURL,
		Listen:      listen,
		JWTSecret:   secret,
	}, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
