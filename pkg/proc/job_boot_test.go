package proc

import (
	"context"
	"testing"

	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/require"

	"github.com/mittwald/mittnite/internal/config"
)

func newCanFailBootJob(t *testing.T, command string) *BootJob {
	t.Helper()

	job, err := NewBootJob(&config.BootJobConfig{
		BaseJobConfig: config.BaseJobConfig{
			Name:    "can-fail-boot-job",
			Command: command,
			CanFail: true,
		},
	})
	require.NoError(t, err)
	return job
}

// A successful canFail boot job must not log the "allowed to fail" warning.
func TestBootJobCanFailDoesNotWarnOnSuccess(t *testing.T) {
	logHook := logtest.NewGlobal()
	defer logHook.Reset()

	require.NoError(t, newCanFailBootJob(t, "true").Run(context.Background()))

	for _, entry := range logHook.AllEntries() {
		require.NotEqual(t, log.WarnLevel, entry.Level, "unexpected warning: %s", entry.Message)
	}
}

// A failing canFail boot job still warns and reports success.
func TestBootJobCanFailStillWarnsOnFailure(t *testing.T) {
	logHook := logtest.NewGlobal()
	defer logHook.Reset()

	require.NoError(t, newCanFailBootJob(t, "false").Run(context.Background()))

	var warnings []string
	for _, entry := range logHook.AllEntries() {
		if entry.Level == log.WarnLevel {
			warnings = append(warnings, entry.Message)
		}
	}
	require.Contains(t, warnings, "job failed, but is allowed to fail")
}
