package proc

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mittwald/mittnite/internal/config"
)

func watchCommandTestJob(t *testing.T, decorated bool) (*CommonJob, *os.File, *os.File) {
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

	job, err := NewCommonJob(&config.JobConfig{
		BaseJobConfig: config.BaseJobConfig{
			Name:             "watch-job",
			Command:          "true",
			EnableTimestamps: boolPtr(decorated),
			EnableNamePrefix: boolPtr(decorated),
		},
	})
	require.NoError(t, err)
	return job, stdout, stderr
}

// Watch pre/post command output carries the same timestamp/name decoration
// as the job's own output, on both streams, and is fully flushed when the
// command returns.
func TestWatchCommandOutputIsDecorated(t *testing.T) {
	job, stdout, stderr := watchCommandTestJob(t, true)

	cmd := exec.Command("sh", "-c", "echo changed; echo oops >&2")
	require.NoError(t, job.runCommandWithJobDecoration(cmd, stdout, stderr))

	outBytes, err := os.ReadFile(stdout.Name())
	require.NoError(t, err)
	errBytes, err := os.ReadFile(stderr.Name())
	require.NoError(t, err)

	linePattern := `^\[\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(Z|[+-]\d{2}:\d{2})\] \[watch-job\] `
	require.Regexp(t, linePattern+"changed\n$", string(outBytes))
	require.Regexp(t, linePattern+"oops\n$", string(errBytes))
}

// A job with decoration explicitly disabled runs its watch commands with the
// targets attached directly — the output stays byte-identical.
func TestWatchCommandOutputRawWhenUndecorated(t *testing.T) {
	job, stdout, stderr := watchCommandTestJob(t, false)

	cmd := exec.Command("sh", "-c", "echo changed; echo oops >&2")
	require.NoError(t, job.runCommandWithJobDecoration(cmd, stdout, stderr))

	outBytes, err := os.ReadFile(stdout.Name())
	require.NoError(t, err)
	errBytes, err := os.ReadFile(stderr.Name())
	require.NoError(t, err)

	require.Equal(t, "changed\n", string(outBytes))
	require.Equal(t, "oops\n", string(errBytes))
}
