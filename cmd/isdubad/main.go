// This file is Free Software under the Apache-2.0 License
// without warranty, see README.md and LICENSES/Apache-2.0.txt for details.
//
// SPDX-License-Identifier: Apache-2.0
//
// SPDX-FileCopyrightText: 2024 German Federal Office for Information Security (BSI) <https://www.bsi.bund.de>
// Software-Engineering: 2024 Intevation GmbH <https://intevation.de>

// Package main implements the main driver for the isduba server.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/ISDuBA/ISDuBA/pkg/aggregators"
	"github.com/ISDuBA/ISDuBA/pkg/config"
	"github.com/ISDuBA/ISDuBA/pkg/database"
	"github.com/ISDuBA/ISDuBA/pkg/forwarder"
	"github.com/ISDuBA/ISDuBA/pkg/sources"
	"github.com/ISDuBA/ISDuBA/pkg/tempstore"
	"github.com/ISDuBA/ISDuBA/pkg/version"
	"github.com/ISDuBA/ISDuBA/pkg/web"
	"github.com/gocsaf/csaf/v3/csaf"
)

func check(err error) {
	if err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run(cfg *config.Config) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGKILL, syscall.SIGTERM)
	defer stop()

	terminate, err := database.CheckMigrations(ctx, &cfg.Database)
	if err != nil {
		return fmt.Errorf("migrating failed: %w", err)
	}
	if terminate {
		return nil
	}
	db, err := database.NewDB(ctx, &cfg.Database)
	if err != nil {
		return err
	}
	defer db.Close(ctx)
	tmpStore := tempstore.NewStore(&cfg.TempStore)
	go tmpStore.Run(ctx)

	forwardManager := forwarder.NewForwardManager(&cfg.Forwarder, db)
	go forwardManager.Run(ctx)

	agg := aggregators.NewManager(cfg, db)
	go agg.Run(ctx)

	// Is the remote validator configured?
	var val csaf.RemoteValidator
	if cfg.RemoteValidator.URL != "" {
		v, err := cfg.RemoteValidator.Open()
		if err != nil {
			return fmt.Errorf("configuring remote validator failed: %w", err)
		}
		val = csaf.SynchronizedRemoteValidator(v)
		defer val.Close()
	}

	// Setup the source manager.
	sm, err := sources.NewManager(cfg, db, val)
	if err != nil {
		return fmt.Errorf("creating source manager failed: %w", err)
	}
	if err := sm.Boot(ctx); err != nil {
		return fmt.Errorf("booting source manager failed: %w", err)
	}
	go sm.Run(ctx)

	cfg.Web.Configure()

	ctrl := web.NewController(
		cfg, db,
		forwardManager,
		tmpStore,
		sm,
		agg,
		val,
	)

	addr := cfg.Web.Addr()
	slog.Info("Starting web server", "address", addr)
	srv := &http.Server{
		Addr:    addr,
		Handler: ctrl.Bind(),
	}

	// Check if we should serve on an unix domain socket.
	var listener net.Listener
	if host := cfg.Web.Host; filepath.IsAbs(host) {
		host = strings.ReplaceAll(host, "{port}", strconv.Itoa(cfg.Web.Port))
		l, err := net.Listen("unix", host)
		if err != nil {
			return fmt.Errorf("cannot listen on domain socket: %w", err)
		}
		defer func() {
			l.Close()
			// Cleanup socket file
			os.Remove(host)
		}()
		// Enable writing to socket
		if err := os.Chmod(host, 0777); err != nil {
			return fmt.Errorf("cannot change rights on socket: %w", err)
		}
		listener = l
	}

	srvErrors := make(chan error)

	done := make(chan struct{})
	go func() {
		defer close(done)
		serve := srv.ListenAndServe
		if listener != nil {
			serve = func() error { return srv.Serve(listener) }
		}
		if err := serve(); err != http.ErrServerClosed {
			srvErrors <- err
		}
	}()

	select {
	case <-ctx.Done():
		slog.Info("Shutting down")
		srv.Shutdown(ctx)
	case err = <-srvErrors:
	}
	<-done
	return err
}

func main() {
	var (
		cfgFile     string
		showVersion bool
	)
	flag.StringVar(&cfgFile, "config", config.DefaultConfigFile, "configuration file")
	flag.StringVar(&cfgFile, "c", config.DefaultConfigFile, "configuration file (shorthand)")
	flag.BoolVar(&showVersion, "version", false, "show version")
	flag.BoolVar(&showVersion, "V", false, "show version (shorthand)")
	flag.Parse()
	if showVersion {
		fmt.Printf("%s version: %s\n", os.Args[0], version.SemVersion)
		os.Exit(0)
	}
	cfg, err := config.Load(cfgFile)
	check(err)
	check(cfg.Log.Config())
	check(run(cfg))
}
