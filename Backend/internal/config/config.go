package config

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

type AppConfig struct {
	Port        string
	DatabaseURL string
}

func Load() AppConfig {
	_ = godotenv.Load() // load .env if present
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("missing required env: DATABASE_URL")
	}
	return AppConfig{
		Port:        port,
		DatabaseURL: dbURL,
	}
}
