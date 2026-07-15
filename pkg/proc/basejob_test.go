package proc

import (
	"context"
	"syscall"
	"testing"
	"time"

	"github.com/mittwald/mittnite/internal/config"
	"github.com/stretchr/testify/require"
)

func startTestJob(t *testing.T) (*baseJob, chan error) {
	t.Helper()

	job, err := newBaseJob(&config.BaseJobConfig{
		Name:    "test-job",
		Command: "sleep",
		Args:    []string{"30"},
	})
	require.NoError(t, err)

	errChan := make(chan error, 1)
	go func() {
		errChan <- job.startOnce(context.Background(), nil)
	}()

	require.Eventually(t, func() bool {
		return job.cmd != nil && job.cmd.Process != nil && syscall.Kill(job.cmd.Process.Pid, 0) == nil
	}, 5*time.Second, 20*time.Millisecond, "process should be running")

	return job, errChan
}

func TestStartOnceReportsExpectedStopAsIntentional(t *testing.T) {
	job, errChan := startTestJob(t)

	job.stopExpected = true
	job.Signal(syscall.SIGTERM)

	select {
	case err := <-errChan:
		require.ErrorIs(t, err, ProcessStoppedIntentionallyError)
	case <-time.After(5 * time.Second):
		t.Fatal("startOnce did not return")
	}
}

func TestStartOnceReportsUnexpectedSignalAsError(t *testing.T) {
	job, errChan := startTestJob(t)

	job.Signal(syscall.SIGTERM)

	select {
	case err := <-errChan:
		require.Error(t, err)
		require.NotErrorIs(t, err, ProcessStoppedIntentionallyError)
		require.EqualError(t, err, "signal: terminated")
	case <-time.After(5 * time.Second):
		t.Fatal("startOnce did not return")
	}
}
