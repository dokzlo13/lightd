package main

import (
	"flag"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/dokzlo13/lightd/internal/app"
	"github.com/dokzlo13/lightd/internal/config"
)

func main() {
	// Support both -c and --config for config path
	var configPath string
	flag.StringVar(&configPath, "config", "config.yaml", "Path to configuration file")
	flag.StringVar(&configPath, "c", "config.yaml", "Path to configuration file (shorthand)")
	resetState := flag.Bool("reset-state", false, "Clear stored desired state (bank scenes) on startup")
	flag.Parse()

	// Load configuration
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load configuration")
	}

	// Setup logging
	setupLogging(cfg.Log.GetLevel(), cfg.Log.UseJSON, cfg.Log.Colors)

	log.Info().Str("config", configPath).Msg("Starting lightd")

	// Create application
	application, err := app.New(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create application")
	}

	// Handle reset state flag
	if *resetState {
		log.Info().Msg("Clearing stored desired state (--reset-state)")
		if err := application.ClearDesiredState(); err != nil {
			log.Warn().Err(err).Msg("Failed to clear desired state")
		}
	}

	// Create context that cancels on shutdown signal
	ctx := app.SignalContext()

	// Start the application
	if err := application.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("Failed to start application")
	}

	// Wait for shutdown
	application.Wait()

	// Graceful shutdown
	if err := application.Stop(); err != nil {
		log.Error().Err(err).Msg("Error during shutdown")
	}
}

func setupLogging(level string, useJSON bool, colors bool) {
	// ISO 8601 format with timezone
	zerolog.TimeFieldFormat = time.RFC3339

	if useJSON {
		// JSON output for production
		log.Logger = zerolog.New(os.Stderr).With().Timestamp().Logger()
	} else {
		// Text output (with optional colors)
		log.Logger = log.Output(zerolog.ConsoleWriter{
			Out:        os.Stderr,
			TimeFormat: "2006-01-02T15:04:05.000Z07:00",
			NoColor:    !colors,
		})
	}

	switch level {
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "info":
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case "warn":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	default:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
}
