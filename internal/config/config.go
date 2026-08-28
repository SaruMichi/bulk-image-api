package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port          string
	DatabaseURL   string
	StorageDir    string // root folder: storage/original & storage/result
	MaxImageWidth int
	WatermarkPath string // kosong = watermark dimatikan
}

func Load() Config {
	return Config{
		Port:          getEnv("PORT", "8080"),
		DatabaseURL:   getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/bulk_image_api?sslmode=disable"),
		StorageDir:    getEnv("STORAGE_DIR", "storage"),
		MaxImageWidth: getEnvInt("MAX_IMAGE_WIDTH", 1600),
		WatermarkPath: getEnv("WATERMARK_PATH", ""),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return i
}
