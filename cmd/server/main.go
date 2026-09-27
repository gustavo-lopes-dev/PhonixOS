package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gustavo-lopes-dev/PhonixOS/internal/api"
	"github.com/gustavo-lopes-dev/PhonixOS/internal/api/handlers"
	"github.com/gustavo-lopes-dev/PhonixOS/internal/api/ws"
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

	hub := ws.NewHub(systemProfile)
	broadcaster := ws.NewBroadcaster(hub)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	broadcastDone := make(chan struct{})
	go func() {
		defer close(broadcastDone)
		broadcaster.Run(ctx)
	}()

	app := api.NewServer(api.Routes{
		Health: system.Health, Profile: system.GetProfile, Metrics: system.Metrics,
		ListShortcuts: shortcuts.List, CreateShortcut: shortcuts.Create,
		UpdateShortcut: shortcuts.Update, DeleteShortcut: shortcuts.Delete,
		GetSettings: settings.Get, PatchSettings: settings.Patch,
		GetLayout: settings.GetLayout, PutLayout: settings.PutLayout,
	}, hub)

	listenErr := make(chan error, 1)
	go func() { listenErr <- app.Listen(":8080") }()

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(shutdown)

	select {
	case err := <-listenErr:
		cancel()
		<-broadcastDone
		hub.CloseAll()
		return err
	case <-shutdown:
		slog.Info("shutting down server")
		cancel()
		<-broadcastDone
		hub.CloseAll()
		if err := app.ShutdownWithTimeout(5 * time.Second); err != nil {
			return err
		}
		return <-listenErr
	}
}
