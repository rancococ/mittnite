package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/mittwald/mittnite/internal/config"
	"github.com/mittwald/mittnite/pkg/files"
	"github.com/mittwald/mittnite/pkg/pidfile"
	"github.com/mittwald/mittnite/pkg/probe"
	"github.com/mittwald/mittnite/pkg/proc"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	DefaultAPIAddress = "unix:///var/run/mittnite.sock"

	envJobLogTimestamps = "MITTNITE_JOB_LOG_TIMESTAMPS"
	envJobLogNamePrefix = "MITTNITE_JOB_LOG_NAME_PREFIX"

	// job output decoration is on by default since v2.0.0; see the
	// migration section in the README
	defaultJobLogTimestamps = true
	defaultJobLogNamePrefix = true
)

var (
	probeListenPort  int
	pidFile          string
	apiEnabled       bool
	apiListenAddress string
	keepRunning      bool
	jobLogTimestamps bool
	jobLogNamePrefix bool
)

func init() {
	log.StandardLogger().ExitFunc = func(i int) {
		defer func() {
			_ = recover() // prevent from printing trace
		}()
		panic(fmt.Sprintf("exit %d", i))
	}
	rootCmd.AddCommand(up)
	up.PersistentFlags().IntVarP(&probeListenPort, "probe-listen-port", "p", 9102, "set the port to listen for probe requests")
	up.PersistentFlags().StringVarP(&pidFile, "pidfile", "", "", "write mittnites process id to this file")
	up.PersistentFlags().BoolVarP(&apiEnabled, "api", "", false, "enables the api for remote or cli controlling")
	up.PersistentFlags().StringVarP(&apiListenAddress, "api-listen-address", "", DefaultAPIAddress, fmt.Sprintf("listen address for the api. Defaults to %q", DefaultAPIAddress))
	up.PersistentFlags().BoolVarP(&keepRunning, "keep-running", "k", false, "keep mittnite running even if no job is running anymore")
	up.PersistentFlags().BoolVar(&jobLogTimestamps, "job-log-timestamps", envBool(envJobLogTimestamps, defaultJobLogTimestamps), "prefix each output line of every job with a timestamp (RFC3339 unless the job configures a format); disable globally with --job-log-timestamps=false or "+envJobLogTimestamps+"=0; an explicit per-job enableTimestamps wins")
	up.PersistentFlags().BoolVar(&jobLogNamePrefix, "job-log-name-prefix", envBool(envJobLogNamePrefix, defaultJobLogNamePrefix), "prefix each output line of every job with the job's name; disable globally with --job-log-name-prefix=false or "+envJobLogNamePrefix+"=0; an explicit per-job enableNamePrefix wins")
}

// envBool interprets an environment variable as a boolean flag default; unset
// or unparsable values fall back to defaultValue (the latter are warned about
// in Run, since logging is not set up yet when flag defaults are evaluated).
func envBool(key string, defaultValue bool) bool {
	v, err := strconv.ParseBool(os.Getenv(key))
	if err != nil {
		return defaultValue
	}
	return v
}

func warnUnparsableEnvBools() {
	for key, defaultValue := range map[string]bool{
		envJobLogTimestamps: defaultJobLogTimestamps,
		envJobLogNamePrefix: defaultJobLogNamePrefix,
	} {
		if v, ok := os.LookupEnv(key); ok {
			if _, err := strconv.ParseBool(v); err != nil {
				log.Warnf("ignoring environment variable %s: %q is not a boolean value, using default %t", key, v, defaultValue)
			}
		}
	}
}

var up = &cobra.Command{
	Use:   "up",
	Short: "Render config files, start probes and processes",
	Long:  "This sub-command renders the configuration files, starts the probes and launches all configured processes",
	RunE: func(cmd *cobra.Command, args []string) error {
		ignitionConfig := &config.Ignition{
			Probes: nil,
			Files:  nil,
			Jobs:   nil,
		}

		pidFileHandle := pidfile.New(pidFile)

		if err := pidFileHandle.Acquire(); err != nil {
			return fmt.Errorf("failed to write pid file to %q: %w", pidFile, err)
		}

		defer func() {
			if err := pidFileHandle.Release(); err != nil {
				log.Errorf("error while cleaning up the pid file: %s", err)
			}
		}()

		warnUnparsableEnvBools()

		if err := ignitionConfig.GenerateFromConfigDir(configDir); err != nil {
			return fmt.Errorf("failed while trying to generate ignition config from dir '%s': %w", configDir, err)
		}

		ignitionConfig.ApplyJobLogDefaults(jobLogTimestamps, jobLogNamePrefix)

		if err := files.RenderFiles(ignitionConfig.Files); err != nil {
			return fmt.Errorf("failed while rendering files from ignition config, err: %w", err)
		}

		signals := make(chan os.Signal, 1)
		signal.Notify(signals,
			syscall.SIGTERM,
			syscall.SIGINT,
		)

		readinessSignals := make(chan os.Signal, 1)
		probeSignals := make(chan os.Signal, 1)
		procSignals := make(chan os.Signal, 1)

		go func() {
			for s := range signals {
				log.Infof("received event %s", s.String())
				readinessSignals <- s
				probeSignals <- s
				procSignals <- s
			}
		}()

		probeHandler, _ := probe.NewProbeHandler(ignitionConfig)

		go func() {
			log.Infof("probeServer listens on port %d", probeListenPort)

			if err := probe.RunProbeServer(probeHandler, probeSignals, probeListenPort); err != nil {
				log.Fatalf("probe server stopped with error: %s", err)
			} else {
				log.Info("probe server stopped without error")
			}
		}()

		go proc.ReapZombies()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		var api *proc.Api
		if apiEnabled {
			api = proc.NewApi(apiListenAddress)

			defer api.Shutdown()
		}

		runner := proc.NewRunner(ctx, api, keepRunning, ignitionConfig)

		if err := runner.Init(); err != nil {
			return fmt.Errorf("runner failed to initialize: %w", err)
		}

		go func() {
			// start the API BEFORE waiting for readiness signals, so that the API is available
			// even if we're still waiting on some probes to become ready
			if err := runner.StartAPI(); err != nil {
				log.Fatalf("error while starting API: %v", err)
			}
		}()

		if err := probeHandler.Wait(readinessSignals); err != nil {
			return fmt.Errorf("probe handler failed while waiting for readiness signals: %w", err)
		}

		go func() {
			<-procSignals
			cancel()
		}()

		if err := runner.Boot(); err != nil {
			if errors.Is(err, context.Canceled) {
				log.Info("shutdown requested during boot")
				return nil
			}
			log.WithError(err).Fatal("runner error'ed during initialization")
		} else {
			log.Info("initialization complete")
		}

		// a canceled context is the normal SIGTERM/SIGINT shutdown path; the
		// runner has already waited for the jobs' SIGTERM → SIGKILL escalation
		err := runner.Run()
		switch {
		case err == nil:
			log.Print("service runner stopped without error")
		case errors.Is(err, context.Canceled):
			log.Info("service runner shut down gracefully")
		default:
			log.WithError(err).Fatal("service runner stopped with error")
		}

		return nil
	},
}
