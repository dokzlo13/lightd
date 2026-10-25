package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"

	"github.com/dokzlo13/lightd/internal/app"
	"github.com/dokzlo13/lightd/internal/buildinfo"
	"github.com/dokzlo13/lightd/internal/config"
	"github.com/dokzlo13/lightd/internal/geo"
	"github.com/dokzlo13/lightd/internal/scheduler"
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
	policyPath := flags.String("preview-policy", "", "Preview a pure policy file without network or database access")
	date := flags.String("date", "", "Local policy date YYYY-MM-DD")
	zone := flags.String("timezone", "UTC", "IANA timezone for preview")
	latitude := flags.Float64("latitude", math.NaN(), "Preview latitude (explicit, no geocoding)")
	longitude := flags.Float64("longitude", math.NaN(), "Preview longitude (explicit, no geocoding)")
	jsonOutput := flags.Bool("json", false, "Machine-readable policy preview")
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
	if *policyPath != "" {
		return previewPolicy(out, *policyPath, *date, *zone, *latitude, *longitude, *jsonOutput)
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

func previewPolicy(out io.Writer, path, date, zone string, lat, lon float64, asJSON bool) error {
	if math.IsNaN(lat) || math.IsInf(lat, 0) || lat < -90 || lat > 90 || math.IsNaN(lon) || math.IsInf(lon, 0) || lon < -180 || lon > 180 {
		return errors.New("preview requires finite --latitude and --longitude within geographic bounds")
	}
	if date == "" {
		return errors.New("preview requires --date YYYY-MM-DD")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	spec, err := scheduler.LoadPolicyFile(ctx, path)
	if err != nil {
		return err
	}
	calc := geo.NewCalculator(&geo.Location{Name: "preview", Latitude: lat, Longitude: lon, Timezone: zone})
	policy, err := scheduler.CompilePolicy(spec, calc, zone)
	if err != nil {
		return err
	}
	preview, err := policy.Preview(date)
	if err != nil {
		return err
	}
	if asJSON {
		e := json.NewEncoder(out)
		e.SetIndent("", "  ")
		return e.Encode(preview)
	}
	fmt.Fprintf(out, "Policy %s (%s), %s [%s]\n", preview.ID, preview.Version, preview.Date, preview.Timezone)
	for _, phase := range preview.Phases {
		when := "skipped"
		if phase.Time != nil {
			when = phase.Time.Format(time.RFC3339)
		}
		fmt.Fprintf(out, "%-22s %-25s %s %s\n", phase.ID, when, phase.Action, strings.Join(phase.Reasons, ", "))
	}
	return nil
}
