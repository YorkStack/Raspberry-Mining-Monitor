// Command rmm is the Raspberry Mining Monitor backend.
//
// It polls AxeOS miners, a solo pool and a Bitcoin data source, and serves a
// read-only dashboard. It is monitoring infrastructure only: it never sits in
// the mining path, and it has no code path that writes to a miner.
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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/bitcoin"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/bitcoin/mempool"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/collect"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/config"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/dashboard"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/demo"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/httpapi"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/miner"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/miner/axeos"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/pool"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/pool/publicpool"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/settings"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/state"
	"github.com/YorkStack/Raspberry-Mining-Monitor/web"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "rmm:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		configPath  = flag.String("config", "config.yaml", "path to the configuration file")
		demoMode    = flag.Bool("demo", false, "run with simulated miners, pool and network data")
		addrFlag    = flag.String("addr", "", "override the listen address, for example 127.0.0.1:8080")
		logLevel    = flag.String("log-level", "info", "log level: debug, info, warn, error")
		showVersion = flag.Bool("version", false, "print the version and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println("rmm", version)
		return nil
	}

	log := newLogger(*logLevel)

	cfg, err := loadConfig(*configPath, *demoMode)
	if err != nil {
		return err
	}

	names := make([]string, 0, len(cfg.Miners))
	for _, m := range cfg.Miners {
		names = append(names, m.Name)
	}
	store := state.New(names)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup
	startCollectors(ctx, &wg, cfg, store, log)

	addr := *addrFlag
	if addr == "" {
		addr = net.JoinHostPort(cfg.Dashboard.Bind, strconv.Itoa(cfg.Dashboard.Port))
	}

	// Threshold overrides. A broken or missing file must never stop the
	// dashboard, so a load failure is logged and the defaults stay in force.
	bands := settings.New(cfg.Dashboard.SettingsPath, defaultBand(cfg))
	if err := bands.Load(); err != nil {
		log.Warn("could not load threshold overrides, using defaults", "err", err)
	}

	handler := httpapi.NewHandler(httpapi.Options{
		Store:              store,
		Intervals:          intervals(cfg),
		Static:             web.Assets(),
		Version:            version,
		Settings:           bands,
		SettingsEnabled:    cfg.Dashboard.Settings,
		MinerNames:         names,
		ScreensaverSeconds: cfg.Dashboard.ScreensaverMinutes * 60,
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		// No WriteTimeout: SSE connections are long-lived by design.
		IdleTimeout: 120 * time.Second,
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	log.Info("dashboard listening",
		"addr", ln.Addr().String(),
		"demo", cfg.Demo,
		"miners", len(cfg.Miners),
		"version", version)
	if cfg.Dashboard.Settings {
		log.Info("threshold settings page available from this machine only",
			"path", "/settings", "overrides", bands.Path())
	}
	if cfg.Demo {
		log.Info("demo mode: all miner, pool and network data is simulated")
	}

	serveErr := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		stop()
		wg.Wait()
		return err
	case <-ctx.Done():
		log.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Warn("graceful shutdown failed", "err", err)
	}
	wg.Wait()
	return nil
}

func loadConfig(path string, demoMode bool) (config.Config, error) {
	if demoMode {
		return config.Demo(), nil
	}
	cfg, err := config.Load(path)
	if err != nil {
		return config.Config{}, fmt.Errorf("%w\n\nRun with --demo to start without miners", err)
	}
	return cfg, nil
}

// defaultBand is the fleet-wide warning band taken from the first configured
// miner, which is where the config defaults land.
func defaultBand(cfg config.Config) settings.Thresholds {
	b := settings.Thresholds{
		ASICWarnC: config.DefaultWarnTempC,
		ASICCritC: config.DefaultCritTempC,
		VRMWarnC:  config.DefaultVRMWarnTempC,
		VRMCritC:  config.DefaultVRMCritTempC,
	}
	if len(cfg.Miners) > 0 {
		m := cfg.Miners[0]
		b = settings.Thresholds{
			ASICWarnC: m.WarnTempC,
			ASICCritC: m.CritTempC,
			VRMWarnC:  m.VRMWarnTempC,
			VRMCritC:  m.VRMCritTempC,
		}
	}
	return b
}

func intervals(cfg config.Config) dashboard.Input {
	in := dashboard.Input{
		MinerInterval:   config.DefaultMinerInterval,
		PoolInterval:    cfg.Pool.Interval,
		NetworkInterval: cfg.Bitcoin.Interval,
		Thresholds: dashboard.Thresholds{
			ASICWarnC: config.DefaultWarnTempC,
			ASICCritC: config.DefaultCritTempC,
			VRMWarnC:  config.DefaultVRMWarnTempC,
			VRMCritC:  config.DefaultVRMCritTempC,
		},
	}
	// Staleness uses the slowest configured miner interval so a deliberately
	// slow-polled device is not permanently flagged as stale.
	for _, m := range cfg.Miners {
		if m.Interval > in.MinerInterval {
			in.MinerInterval = m.Interval
		}
	}
	return in
}

func startCollectors(ctx context.Context, wg *sync.WaitGroup, cfg config.Config, store *state.Store, log *slog.Logger) {
	clock := time.Now

	for i, m := range cfg.Miners {
		var c miner.Collector
		switch m.Type {
		case "demo":
			c = demo.NewMiner(demo.MinerConfig{
				Name:          m.Name,
				Model:         m.Model,
				Firmware:      "demo",
				NominalTHs:    m.NominalTHs,
				NominalW:      m.NominalW,
				NominalTempC:  m.NominalTempC,
				Fans:          m.Fans,
				DropoutChance: 0.004,
			}, int64(i+1), clock)
		case "axeos":
			c = axeos.New(axeos.Config{
				Name:    m.Name,
				BaseURL: minerBaseURL(m.Host),
				Timeout: m.Timeout,
			})
		default:
			log.Warn("unknown miner type, reporting it as offline",
				"miner", m.Name, "type", m.Type)
			store.FailMiner(m.Name, time.Now(), "unknown miner type "+m.Type)
			continue
		}

		wg.Add(1)
		go func(c miner.Collector, interval, timeout time.Duration) {
			defer wg.Done()
			collect.RunMiner(ctx, c, store, interval, timeout, log)
		}(c, m.Interval, m.Timeout)
	}

	var poolAdapter pool.Adapter
	switch cfg.Pool.Provider {
	case "demo":
		names := make([]string, 0, len(cfg.Miners))
		for _, m := range cfg.Miners {
			names = append(names, m.Name)
		}
		poolAdapter = demo.NewPool(names, 42, clock)
	case "publicpool":
		targets := make([]publicpool.Target, 0, len(cfg.Miners))
		for _, m := range cfg.Miners {
			if m.PayoutAddress == "" {
				log.Warn("miner has no payout_address, excluded from pool stats", "miner", m.Name)
				continue
			}
			targets = append(targets, publicpool.Target{MinerName: m.Name, Address: m.PayoutAddress})
		}
		if len(targets) == 0 {
			log.Warn("publicpool selected but no miner has a payout_address; pool panel will be empty")
			store.FailPool(time.Now(), "no payout addresses configured")
		} else {
			poolAdapter = publicpool.New(publicpool.Config{
				BaseURL: cfg.Pool.BaseURL,
				Timeout: cfg.Pool.Timeout,
				Targets: targets,
			})
		}
	case "none":
		// Pool panel intentionally disabled.
	default:
		log.Warn("unknown pool provider, pool panel will be empty", "provider", cfg.Pool.Provider)
		store.FailPool(time.Now(), "unknown pool provider "+cfg.Pool.Provider)
	}
	if poolAdapter != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			collect.RunPool(ctx, poolAdapter, store, cfg.Pool.Interval, cfg.Pool.Timeout, log)
		}()
	}

	var btc bitcoin.Provider
	switch cfg.Bitcoin.Provider {
	case "demo":
		btc = demo.NewBitcoin(7, clock)
	case "public":
		btc = mempool.New(mempool.Config{
			BaseURL: cfg.Bitcoin.BaseURL,
			Timeout: cfg.Bitcoin.Timeout,
		})
	default:
		log.Warn("bitcoin provider not implemented, network panel will be empty",
			"provider", cfg.Bitcoin.Provider)
		store.FailNetwork(time.Now(), "unknown bitcoin provider "+cfg.Bitcoin.Provider)
	}
	if btc != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			collect.RunNetwork(ctx, btc, store, cfg.Bitcoin.Interval, cfg.Bitcoin.Timeout, log)
		}()
	}
}

// minerBaseURL turns a configured host into an AxeOS base URL. A bare host or
// IP gets http:// and the default port; an explicit scheme is left alone.
func minerBaseURL(host string) string {
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		return strings.TrimRight(host, "/")
	}
	return "http://" + host
}

func newLogger(level string) *slog.Logger {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		l = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l}))
}
