package proc

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mittwald/mittnite/internal/config"
	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/require"
)

func boolPtr(b bool) *bool { return &b }

func startTestJob(t *testing.T) (*baseJob, chan error) {
	t.Helper()

	job := &baseJob{}
	err := job.init(&config.BaseJobConfig{
		Name:    "test-job",
		Command: "sleep",
		Args:    []string{"30"},
	})
	require.NoError(t, err)

	// receiving from the process channel synchronizes with startOnce having
	// set job.cmd, so the job can safely be signaled afterwards
	p := make(chan *os.Process, 1)
	errChan := make(chan error, 1)
	go func() {
		errChan <- job.startOnce(context.Background(), p)
	}()

	select {
	case proc := <-p:
		require.NotNil(t, proc, "process should be running")
	case <-time.After(5 * time.Second):
		t.Fatal("process did not start")
	}

	return job, errChan
}

// A job's forked children inherit the stdout/stderr pipes. Output they write
// after the main process exited must still be forwarded, and the readers must
// not report an error just because the job exited (cmd.StdoutPipe would be
// closed by cmd.Wait mid-read, producing "read |0: file already closed").
func TestStartOnceKeepsLoggingOutputOfLingeringChildren(t *testing.T) {
	logHook := logtest.NewGlobal()
	defer logHook.Reset()

	job := &baseJob{}
	err := job.init(&config.BaseJobConfig{
		Name: "lingering-job",
		// the child traps TERM because startOnce signals the job's process
		// group once the main process has exited; the main process sleeps
		// briefly so the trap is installed before the signal arrives
		Command:          "sh",
		Args:             []string{"-c", "(trap '' TERM; sleep 0.3; echo lingering) & echo main; sleep 0.2"},
		EnableTimestamps: boolPtr(true),
	})
	require.NoError(t, err)

	// stand-in for the passthrough case (job.stdout = os.Stdout), which
	// closeStdFiles leaves open when startOnce returns
	logFile := filepath.Join(t.TempDir(), "stdout.log")
	out, err := os.Create(logFile)
	require.NoError(t, err)
	defer out.Close()
	job.stdout = out
	job.stderr = out

	// returns as soon as the main sh process exits, well before the forked
	// child writes its line
	require.NoError(t, job.startOnce(context.Background(), nil))

	require.Eventually(t, func() bool {
		content, err := os.ReadFile(logFile)
		return err == nil &&
			strings.Contains(string(content), "main") &&
			strings.Contains(string(content), "lingering")
	}, 5*time.Second, 50*time.Millisecond, "output of the lingering child should still be forwarded")

	for _, entry := range logHook.AllEntries() {
		require.NotEqual(t, "error reading from process", entry.Message)
	}
}

// When the job's log target is file-backed, closeStdFiles closes it as soon as
// the job stops. A child that outlived the job and keeps writing must not be
// killed by SIGPIPE: the reader keeps draining the pipe and discards the
// output instead of closing its read end.
func TestStartOnceDrainsPipeAfterLogTargetCloses(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")

	job := &baseJob{}
	err := job.init(&config.BaseJobConfig{
		Name:    "draining-job",
		Command: "sh",
		// the second write happens well after the reader saw the closed log
		// target; if the read end were closed by then, the write would raise
		// SIGPIPE and the marker file would never be created. The main process
		// sleeps briefly so the child has installed its TERM trap before
		// startOnce signals the process group.
		Args: []string{"-c", fmt.Sprintf(
			"(trap '' TERM; sleep 0.3; echo lingering; sleep 0.3; echo again; : > %q) & echo main; sleep 0.2", marker,
		)},
		EnableTimestamps: boolPtr(true),
		Stdout:           filepath.Join(dir, "stdout.log"),
	})
	require.NoError(t, err)

	require.NoError(t, job.startOnce(context.Background(), nil))

	require.Eventually(t, func() bool {
		_, err := os.Stat(marker)
		return err == nil
	}, 5*time.Second, 50*time.Millisecond, "lingering child should survive writing after the log target closed")
}

// Boot jobs used to skip baseJob.init, leaving job.stdout/job.stderr as typed
// nil *os.File values; os/exec turns those into closed file descriptors in the
// child, so all boot job output was silently lost.
func TestNewBootJobInitializesStdStreams(t *testing.T) {
	job, err := NewBootJob(&config.BootJobConfig{
		BaseJobConfig: config.BaseJobConfig{Name: "boot-job", Command: "true"},
	})
	require.NoError(t, err)
	require.Same(t, os.Stdout, job.stdout)
	require.Same(t, os.Stderr, job.stderr)
}

// An unset timestampFormat is the documented default (RFC3339) and must not
// trigger the unknown-format warning.
func TestResolveTimestampLayoutDefaultsToRFC3339WithoutWarning(t *testing.T) {
	logHook := logtest.NewGlobal()
	defer logHook.Reset()

	job := &baseJob{Config: &config.BaseJobConfig{Name: "default-format-job"}}
	layout := job.resolveTimestampLayout(log.WithField("job.name", job.Config.Name))

	require.Equal(t, time.RFC3339, layout)
	for _, entry := range logHook.AllEntries() {
		require.NotEqual(t, log.WarnLevel, entry.Level, "unexpected warning: %s", entry.Message)
	}
}

func TestResolveTimestampLayoutWarnsOnUnknownFormat(t *testing.T) {
	logHook := logtest.NewGlobal()
	defer logHook.Reset()

	job := &baseJob{Config: &config.BaseJobConfig{
		Name:            "unknown-format-job",
		TimestampFormat: "bogus",
	}}
	layout := job.resolveTimestampLayout(log.WithField("job.name", job.Config.Name))

	require.Equal(t, time.RFC3339, layout)

	var warnings []string
	for _, entry := range logHook.AllEntries() {
		if entry.Level == log.WarnLevel {
			warnings = append(warnings, entry.Message)
		}
	}
	require.Contains(t, warnings, "unknown timestamp format, defaulting to RFC3339")
}

func TestResolveTimestampLayoutPrefersCustomFormat(t *testing.T) {
	job := &baseJob{Config: &config.BaseJobConfig{
		Name:                  "custom-format-job",
		TimestampFormat:       "Kitchen",
		CustomTimestampFormat: "2006-01-02",
	}}
	layout := job.resolveTimestampLayout(log.WithField("job.name", job.Config.Name))

	require.Equal(t, "2006-01-02", layout)
}

// forwardOutput must not abort on lines longer than its read buffer: the
// timestamp and name prefix are written once per line, overlong lines arrive
// in chunks, and empty as well as unterminated final lines are forwarded as
// lines.
func TestForwardOutputHandlesOverlongLines(t *testing.T) {
	logHook := logtest.NewGlobal()
	defer logHook.Reset()

	payload := strings.Repeat("a", 200*1024)
	input := "first\n" + payload + "\n\nlast"

	var buf bytes.Buffer
	job := &baseJob{Config: &config.BaseJobConfig{Name: "long-line-job"}}
	job.forwardOutput(io.NopCloser(strings.NewReader(input)), &buf, "2006", []byte("[long-line-job] "))

	prefix := fmt.Sprintf("[%d] [long-line-job] ", time.Now().Year())
	require.Equal(t,
		prefix+"first\n"+prefix+payload+"\n"+prefix+"\n"+prefix+"last\n",
		buf.String())

	for _, entry := range logHook.AllEntries() {
		require.NotEqual(t, "error reading from process", entry.Message)
	}
}

func TestStartOncePrefixesOutputWithTimestampAndJobName(t *testing.T) {
	job := &baseJob{}
	err := job.init(&config.BaseJobConfig{
		Name:             "prefix-job",
		Command:          "sh",
		Args:             []string{"-c", "echo hello"},
		EnableTimestamps: boolPtr(true),
		EnableNamePrefix: boolPtr(true),
	})
	require.NoError(t, err)

	// stand-in for the passthrough case (job.stdout = os.Stdout), which
	// closeStdFiles leaves open when startOnce returns
	logFile := filepath.Join(t.TempDir(), "stdout.log")
	out, err := os.Create(logFile)
	require.NoError(t, err)
	defer out.Close()
	job.stdout = out
	job.stderr = out

	require.NoError(t, job.startOnce(context.Background(), nil))

	// the forwarder goroutine may still be flushing when startOnce returns
	pattern := `^\[\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(Z|[+-]\d{2}:\d{2})\] \[prefix-job\] hello\n$`
	require.Eventually(t, func() bool {
		content, err := os.ReadFile(logFile)
		return err == nil && regexp.MustCompile(pattern).Match(content)
	}, 5*time.Second, 50*time.Millisecond, "output should carry an RFC3339 timestamp and the job name")
}

func TestStartOncePrefixesOutputWithJobNameOnly(t *testing.T) {
	job := &baseJob{}
	err := job.init(&config.BaseJobConfig{
		Name:             "name-only-job",
		Command:          "sh",
		Args:             []string{"-c", "echo hello"},
		EnableNamePrefix: boolPtr(true),
	})
	require.NoError(t, err)

	logFile := filepath.Join(t.TempDir(), "stdout.log")
	out, err := os.Create(logFile)
	require.NoError(t, err)
	defer out.Close()
	job.stdout = out
	job.stderr = out

	require.NoError(t, job.startOnce(context.Background(), nil))

	require.Eventually(t, func() bool {
		content, err := os.ReadFile(logFile)
		return err == nil && string(content) == "[name-only-job] hello\n"
	}, 5*time.Second, 50*time.Millisecond, "output should carry the job name and no timestamp")
}

func TestStartOnceReportsExpectedStopAsIntentional(t *testing.T) {
	job, errChan := startTestJob(t)

	job.stopExpected.Store(true)
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
