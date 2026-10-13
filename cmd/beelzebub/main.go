package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/GeorgeDeveloperJ/Beelzebub/pkg/config"
	"github.com/GeorgeDeveloperJ/Beelzebub/pkg/engine"
	"github.com/GeorgeDeveloperJ/Beelzebub/pkg/store"
	"github.com/GeorgeDeveloperJ/Beelzebub/pkg/tui"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	defaultConfigPath = "sentinels.yaml"
	defaultDBPath     = "sentinel.db"
)

func main() {
	if len(os.Args) < 2 {
		runTUI(defaultConfigPath, defaultDBPath)
		return
	}

	subcommand := os.Args[1]

	switch subcommand {
	case "daemon":
		fs := flag.NewFlagSet("daemon", flag.ExitOnError)
		cfgPath := fs.String("config", defaultConfigPath, "Path to sentinels.yaml configuration file")
		dbPath := fs.String("db", defaultDBPath, "Path to SQLite database file")
		_ = fs.Parse(os.Args[2:])
		runDaemon(*cfgPath, *dbPath)

	case "tui":
		fs := flag.NewFlagSet("tui", flag.ExitOnError)
		cfgPath := fs.String("config", defaultConfigPath, "Path to sentinels.yaml configuration file")
		dbPath := fs.String("db", defaultDBPath, "Path to SQLite database file")
		_ = fs.Parse(os.Args[2:])
		runTUI(*cfgPath, *dbPath)

	case "version":
		fmt.Println("Beelzebub Sentinel v1.0.0 (Self-Healing Homelab SRE Cockpit)")

	case "help", "-h", "--help":
		printUsage()

	default:
		// If first arg ends with .yaml, assume TUI or daemon with that config
		if filepath.Ext(subcommand) == ".yaml" || filepath.Ext(subcommand) == ".yml" {
			runTUI(subcommand, defaultDBPath)
		} else {
			fmt.Printf("Unknown command: %q\n\n", subcommand)
			printUsage()
			os.Exit(1)
		}
	}
}

func printUsage() {
	fmt.Println("Usage: beelzebub <command> [options]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  daemon    Run headlessly as a background service/daemon")
	fmt.Println("  tui       Launch interactive operator terminal dashboard (default)")
	fmt.Println("  version   Display version information")
	fmt.Println("  help      Display this help menu")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  --config  Path to sentinels.yaml (default: sentinels.yaml)")
	fmt.Println("  --db      Path to SQLite database (default: sentinel.db)")
}

func runDaemon(cfgPath, dbPath string) {
	log.Printf("[Beelzebub] Starting daemon mode. Loading config from %q...", cfgPath)

	cfg, err := config.LoadFromFile(cfgPath)
	if err != nil {
		log.Fatalf("[FATAL] Failed to load configuration: %v", err)
	}

	st, err := store.NewStore(dbPath)
	if err != nil {
		log.Fatalf("[FATAL] Failed to initialize SQLite store: %v", err)
	}
	defer st.Close()

	eng := engine.New(cfg, st)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)

	if err := eng.Start(ctx); err != nil {
		log.Fatalf("[FATAL] Failed to start engine: %v", err)
	}

	log.Printf("[Beelzebub] Daemon initialized. Monitoring %d sentinels.", len(cfg.Sentinels))

	sig := <-sigChan
	log.Printf("[Beelzebub] Received signal %v. Initiating graceful shutdown...", sig)
	eng.Stop()
	log.Println("[Beelzebub] Daemon stopped cleanly.")
}

func runTUI(cfgPath, dbPath string) {
	// If config file exists, load it; otherwise create minimal placeholder
	var cfg *config.Config
	var err error

	if _, errStat := os.Stat(cfgPath); errStat == nil {
		cfg, err = config.LoadFromFile(cfgPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config %q: %v\n", cfgPath, err)
			os.Exit(1)
		}
	} else {
		// Use empty configuration with defaults
		cfg = &config.Config{
			Version: "1",
			Global: config.GlobalConfig{
				CheckIntervalDefault: config.Duration(60),
			},
			Sentinels: []config.SentinelConfig{},
		}
	}

	st, err := store.NewStore(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing SQLite store %q: %v\n", dbPath, err)
		os.Exit(1)
	}
	defer st.Close()

	eng := engine.New(cfg, st)

	// Launch Bubbletea program
	p := tea.NewProgram(tui.New(cfg, st, eng), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}
}
