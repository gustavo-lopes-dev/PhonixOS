package database

import (
	"fmt"
	"os"
	"path/filepath"
)

const dbFileName = "phonix.db"

func ResolveDBPath() (string, error) {
	if appDataDir := os.Getenv("APP_DATA_DIR"); appDataDir != "" {
		return resolveInDir(appDataDir)
	}
	if homeDir := os.Getenv("HOME"); homeDir != "" {
		return resolveInDir(filepath.Join(homeDir, "data"))
	}
	return resolveInDir("data")
}

func resolveInDir(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create database directory %q: %w", dir, err)
	}
	return filepath.Join(dir, dbFileName), nil
}
