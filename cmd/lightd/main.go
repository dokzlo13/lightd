package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/dokzlo13/lightd/internal/app"
	"github.com/dokzlo13/lightd/internal/buildinfo"
	"github.com/dokzlo13/lightd/internal/config"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/parse"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		log.Error().Err(err).Msg("lightd stopped")
		os.Exit(1)
	}
}
func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("lightd", flag.ContinueOnError)
	var configPath string
	flags.StringVar(&configPath, "config", "config.yaml", "Configuration file")
	flags.StringVar(&configPath, "c", "config.yaml", "Configuration file")
	version := flags.Bool("version", false, "Print build identity without loading configuration")
	check := flags.Bool("check-config", false, "Validate configuration and Lua syntax without executing the script")
	reset := flags.Bool("reset-state", false, "Clear desired state before starting")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	if *version {
		return json.NewEncoder(out).Encode(buildinfo.Info())
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	if *check {
		if err := checkLuaSyntax(cfg.GetScript()); err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, "Configuration and Lua syntax are valid; dynamic registrations and startup actions were not executed.")
		return err
	}
	setupLogging(cfg.Log.GetLevel(), cfg.Log.UseJSON, cfg.Log.Colors)
	log.Info().Interface("build", buildinfo.Info()).Msg("Starting lightd")
	application, err := app.New(cfg)
	if err != nil {
		return err
	}
	if *reset {
		if err := application.ClearDesiredState(); err != nil {
			_ = application.Stop()
			return fmt.Errorf("reset desired state: %w", err)
		}
	}
	if err := application.Start(app.SignalContext()); err != nil {
		return err
	}
	waitErr := application.Wait()
	stopErr := application.Stop()
	return errors.Join(waitErr, stopErr)
}
func checkLuaSyntax(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() > 1<<20 {
		return errors.New("Lua script exceeds 1 MiB syntax-check limit")
	}
	tree, err := parse.Parse(f, path)
	if err != nil {
		return err
	}
	_, err = lua.Compile(tree, path)
	return err
}
func setupLogging(level string, useJSON, colors bool) {
	zerolog.TimeFieldFormat = time.RFC3339
	if useJSON {
		log.Logger = zerolog.New(os.Stderr).With().Timestamp().Logger()
	} else {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339Nano, NoColor: !colors})
	}
	parsed, err := zerolog.ParseLevel(level)
	if err != nil {
		parsed = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(parsed)
}

// Context is referenced here so CLI preview can share the bounded pure-loader path.
var _ = context.Background
