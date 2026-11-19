// Command pos-print-agent prints ESC/POS bytes for the Lnkits POS web app over a
// loopback HTTP endpoint. It runs as a Windows service (LocalSystem) so it can
// administer print queues, and it never accepts connections from outside 127.0.0.1.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mhkhizil/vision-pos-print-agent/internal/config"
	"github.com/mhkhizil/vision-pos-print-agent/internal/httpapi"
	"github.com/mhkhizil/vision-pos-print-agent/internal/logging"
	"github.com/mhkhizil/vision-pos-print-agent/internal/service"
)

// version is stamped at build time with -ldflags "-X main.version=1.0.0".
var version = "dev"

type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }

func (m *multiFlag) Set(value string) error {
	*m = append(*m, value)
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "pos-print-agent: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		configPath   string
		listen       string
		token        string
		logLevel     string
		origins      multiFlag
		install      bool
		uninstall    bool
		serviceMode  bool
		showVersion  bool
	)
	flag.StringVar(&configPath, "config", "", `path to config.json (default: %PROGRAMDATA%\LnkitsPrintAgent\config.json)`)
	flag.StringVar(&listen, "listen", "", "loopback address to listen on, e.g. 127.0.0.1:8735")
	flag.StringVar(&token, "token", "", "override the access token (development only)")
	flag.StringVar(&logLevel, "log-level", "", "debug, info, warn or error")
	flag.Var(&origins, "allow-origin", "browser origin allowed to print; repeatable; '*' allows every origin")
	flag.BoolVar(&install, "install", false, "install or update the Windows service and exit")
	flag.BoolVar(&uninstall, "uninstall", false, "remove the Windows service and exit")
	flag.BoolVar(&serviceMode, "service", false, "internal: run as a Windows service")
	flag.BoolVar(&showVersion, "version", false, "print the agent version and exit")
	flag.Parse()

	if showVersion {
		fmt.Println(version)
		return nil
	}

	path := strings.TrimSpace(configPath)
	if path == "" {
		path = config.DefaultPath()
	}

	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	if listen != "" {
		cfg.Listen = listen
	}
	if token != "" {
		cfg.Token = token
	}
	if logLevel != "" {
		cfg.LogLevel = logLevel
	}
	if len(origins) > 0 {
		cfg.AllowOrigins = origins
	}

	logger, closer, err := logging.New(cfg.LogDir, cfg.LogLevel)
	if err != nil {
		return err
	}
	defer closer.Close()
	slog.SetDefault(logger)

	if cfg.AllowsEveryOrigin() {
		logger.Warn("allowOrigins is '*' — any web page on this computer can ask the agent to print. Set allowOrigins to the POS origin to lock it down.")
	}

	exePath, exeErr := os.Executable()
	if exeErr != nil {
		exePath = os.Args[0]
	}

	switch {
	case install:
		if err := config.Save(path, cfg); err != nil {
			return err
		}
		if err := service.Install(exePath, path); err != nil {
			return err
		}
		fmt.Printf("Installed %s.\nConfig: %s\nToken:  %s\n", service.Name, path, cfg.Token)
		return nil
	case uninstall:
		if err := service.Uninstall(); err != nil {
			return err
		}
		fmt.Printf("Removed %s.\n", service.Name)
		return nil
	}

	runServer := func(ctx context.Context) error { return serve(ctx, cfg, logger) }

	if serviceMode {
		logger.Info("starting as a Windows service", "version", version, "listen", cfg.Listen)
		serviceErr := service.Run(runServer)
		if !errors.Is(serviceErr, service.ErrNotRunningAsService) {
			return serviceErr
		}
		// --service was passed by hand instead of by the SCM: keep it usable.
		logger.Warn("--service was used but the service manager did not start this process; running in the foreground")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runServer(ctx)
}

func serve(ctx context.Context, cfg *config.Config, logger *slog.Logger) error {
	httpServer := &http.Server{
		Handler:           httpapi.New(cfg, logger, version).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("cannot listen on %s: %w", cfg.Listen, err)
	}

	// A page that resolves "localhost" to IPv6 must reach the same agent.
	var ipv6 net.Listener
	if port, found := strings.CutPrefix(cfg.Listen, "127.0.0.1:"); found {
		if secondary, listenErr := net.Listen("tcp", "[::1]:"+port); listenErr == nil {
			ipv6 = secondary
			go func() { _ = httpServer.Serve(secondary) }()
		}
	}

	serveErrors := make(chan error, 1)
	go func() {
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErrors <- err
		}
	}()
	logger.Info("listening", "address", cfg.Listen, "version", version)

	select {
	case <-ctx.Done():
	case err := <-serveErrors:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
	if ipv6 != nil {
		_ = ipv6.Close()
	}
	logger.Info("stopped")
	return nil
}
