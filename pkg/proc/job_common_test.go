package proc

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mittwald/mittnite/internal/config"
)

func watchCommandTestTargets(t *testing.T) (*os.File, *os.File) {
	t.Helper()

	dir := t.TempDir()
	stdout, err := os.Create(filepath.Join(dir, "stdout"))
	require.NoError(t, err)
	stderr, err := os.Create(filepath.Join(dir, "stderr"))
	require.NoError(t, err)
	t.Cleanup(func() {
		stdout.Close()
		stderr.Close()
	})
	return stdout, stderr
}

func watchCommandTestJob(t *testing.T, timestamps, namePrefix bool) *CommonJob {
	t.Helper()

	job, err := NewCommonJob(&config.JobConfig{
		BaseJobConfig: config.BaseJobConfig{
			Name:             "watch-job",
			Command:          "true",
			EnableTimestamps: boolPtr(timestamps),
			EnableNamePrefix: boolPtr(namePrefix),
		},
	})
	require.NoError(t, err)
	return job
}

const watchDecoratedLinePattern = `^\[\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(Z|[+-]\d{2}:\d{2})\] \[watch-job\] `

// Watch pre/post command output carries the same timestamp/name decoration
// as the job's own output, on both streams, and is fully flushed when the
// command returns.
func TestWatchCommandOutputIsDecorated(t *testing.T) {
	job := watchCommandTestJob(t, true, true)
	stdout, stderr := watchCommandTestTargets(t)

	cmd := exec.Command("sh", "-c", "echo changed; echo oops >&2")
	require.NoError(t, job.runCommandWithJobDecoration(cmd, stdout, stderr))

	outBytes, err := os.ReadFile(stdout.Name())
	require.NoError(t, err)
	errBytes, err := os.ReadFile(stderr.Name())
	require.NoError(t, err)

	require.Regexp(t, watchDecoratedLinePattern+"changed\n$", string(outBytes))
	require.Regexp(t, watchDecoratedLinePattern+"oops\n$", string(errBytes))
}

// With only one option enabled the other must not leak in: a name-prefix-only
// job decorates watch command output with the prefix alone.
func TestWatchCommandOutputNamePrefixOnly(t *testing.T) {
	job := watchCommandTestJob(t, false, true)
	stdout, stderr := watchCommandTestTargets(t)

	cmd := exec.Command("sh", "-c", "echo changed")
	require.NoError(t, job.runCommandWithJobDecoration(cmd, stdout, stderr))

	outBytes, err := os.ReadFile(stdout.Name())
	require.NoError(t, err)
	require.Equal(t, "[watch-job] changed\n", string(outBytes))
}

// A job with decoration explicitly disabled runs its watch commands with the
// targets attached directly. The command output deliberately has no trailing
// newline: the line forwarder would append one, so byte-identical output here
// proves the raw path, not just an undecorated forwarder.
func TestWatchCommandOutputRawWhenUndecorated(t *testing.T) {
	job := watchCommandTestJob(t, false, false)
	stdout, stderr := watchCommandTestTargets(t)

	cmd := exec.Command("sh", "-c", "printf changed; printf oops >&2")
	require.NoError(t, job.runCommandWithJobDecoration(cmd, stdout, stderr))

	outBytes, err := os.ReadFile(stdout.Name())
	require.NoError(t, err)
	errBytes, err := os.ReadFile(stderr.Name())
	require.NoError(t, err)

	require.Equal(t, "changed", string(outBytes))
	require.Equal(t, "oops", string(errBytes))
}

// When the command cannot be started, the parent-side pipe write ends must
// still be closed so the forwarders see EOF immediately — the error returns
// fast instead of blocking on the full bounded drain.
func TestRunCommandWithJobDecorationStartFailureReturnsFast(t *testing.T) {
	job := watchCommandTestJob(t, true, true)
	stdout, stderr := watchCommandTestTargets(t)

	cmd := exec.Command("/nonexistent-mittnite-test-binary")
	start := time.Now()
	require.Error(t, job.runCommandWithJobDecoration(cmd, stdout, stderr))
	require.Less(t, time.Since(start), 900*time.Millisecond)

	outBytes, err := os.ReadFile(stdout.Name())
	require.NoError(t, err)
	require.Empty(t, outBytes)
}

// executeWatchCommand itself must route through the decoration helper — this
// exercises the real wiring by swapping the process-wide streams it targets.
// (Package tests run sequentially; nothing else writes to os.Stdout here, and
// logrus holds its own reference to the original stderr.)
func TestExecuteWatchCommandRoutesThroughDecoration(t *testing.T) {
	job := watchCommandTestJob(t, true, true)
	stdout, stderr := watchCommandTestTargets(t)

	origStdout, origStderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = stdout, stderr
	defer func() {
		os.Stdout, os.Stderr = origStdout, origStderr
	}()

	err := job.executeWatchCommand(&config.WatchCommand{
		Command: "sh",
		Args:    []string{"-c", "echo changed"},
	})
	os.Stdout, os.Stderr = origStdout, origStderr
	require.NoError(t, err)

	outBytes, readErr := os.ReadFile(stdout.Name())
	require.NoError(t, readErr)
	require.Regexp(t, watchDecoratedLinePattern+"changed\n$", string(outBytes))
}
