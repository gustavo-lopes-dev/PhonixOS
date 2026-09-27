package main

import (
	"log/slog"
	"os"
	"time"

	"github.com/gustavo-lopes-dev/PhonixOS/internal/api"
	"github.com/gustavo-lopes-dev/PhonixOS/internal/api/handlers"
	"github.com/gustavo-lopes-dev/PhonixOS/internal/database"
	"github.com/gustavo-lopes-dev/PhonixOS/internal/network"
	"github.com/gustavo-lopes-dev/PhonixOS/internal/profile"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	path, err := database.ResolveDBPath()
	if err != nil {
		return err
	}
	db, mode, err := database.InitDB(path)
	if err != nil {
		return err
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Error("close database", "error", err)
		}
	}()
	if err := database.RunMigrations(db); err != nil {
		return err
	}
	systemProfile, err := profile.DetectProfile(mode)
	if err != nil {
		return err
	}
	addresses, err := network.LocalIPv4()
	if err != nil {
		slog.Warn("could not detect local IPv4 addresses", "error", err)
	} else if len(addresses) == 0 {
		slog.Warn("no active local IPv4 addresses detected")
	}
	for _, address := range addresses {
		slog.Info("server available", "url", "http://"+address+":8080")
	}
	store := database.NewStore(db)
	system := handlers.System{Profile: systemProfile, Started: time.Now()}
	shortcuts := handlers.Shortcuts{Store: store}
	settings := handlers.Settings{Store: store}
	return api.NewServer(api.Routes{
		Health: system.Health, Profile: system.GetProfile, Metrics: system.Metrics,
		ListShortcuts: shortcuts.List, CreateShortcut: shortcuts.Create,
		UpdateShortcut: shortcuts.Update, DeleteShortcut: shortcuts.Delete,
		GetSettings: settings.Get, PatchSettings: settings.Patch,
		GetLayout: settings.GetLayout, PutLayout: settings.PutLayout,
	}).Listen(":8080")
}
