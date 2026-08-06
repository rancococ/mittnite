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
	job.init(&config.BaseJobConfig{
		Name:    "test-job",
		Command: "sleep",
		Args:    []string{"30"},
	})

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
	job.init(&config.BaseJobConfig{
		Name: "lingering-job",
		// the child traps TERM because startOnce signals the job's process
		// group once the main process has exited; the main process sleeps
		// briefly so the trap is installed before the signal arrives
		Command:          "sh",
		Args:             []string{"-c", "(trap '' TERM; sleep 0.3; echo lingering) & echo main; sleep 0.2"},
		EnableTimestamps: boolPtr(true),
	})

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
	job.init(&config.BaseJobConfig{
		Name:    "draining-job",
		Command: "sh",
		// the child outlives the one-second forwarder flush wait, so its
		// second write happens well after the log target was closed; if the
		// read end were closed by then, the write would raise SIGPIPE and the
		// marker file would never be created. The main process sleeps briefly
		// so the child has installed its TERM trap before startOnce signals
		// the process group.
		Args: []string{"-c", fmt.Sprintf(
			"(trap '' TERM; sleep 0.3; echo lingering; sleep 1.2; echo again; : > %q) & echo main; sleep 0.2", marker,
		)},
		EnableTimestamps: boolPtr(true),
		Stdout:           filepath.Join(dir, "stdout.log"),
	})

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

// Construction validates configured log paths but must not keep the files
// open: startOnce opens its own handles on every run and would overwrite the
// constructor-opened pair without closing it, leaking the descriptors.
func TestNewCommonJobValidatesLogTargetsWithoutKeepingThemOpen(t *testing.T) {
	job, err := NewCommonJob(&config.JobConfig{
		BaseJobConfig: config.BaseJobConfig{
			Name:    "validated-job",
			Command: "true",
			Stdout:  filepath.Join(t.TempDir(), "stdout.log"),
		},
	})
	require.NoError(t, err)
	require.Same(t, os.Stdout, job.stdout, "no file handle should be held after construction")

	parent := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(parent, nil, 0o644))
	_, err = NewCommonJob(&config.JobConfig{
		BaseJobConfig: config.BaseJobConfig{
			Name:    "broken-target-job",
			Command: "true",
			Stdout:  filepath.Join(parent, "stdout.log"),
		},
	})
	require.Error(t, err, "broken log paths should still fail at config load")
}

// A boot job's configured log files are only opened in startOnce, so an
// unopenable path flows through Run's canFail handling instead of failing the
// construction — which would abort mittnite's entire boot.
func TestBootJobWithUnopenableLogTargetHonorsCanFail(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(parent, nil, 0o644))

	newJob := func(canFail bool) *BootJob {
		job, err := NewBootJob(&config.BootJobConfig{
			BaseJobConfig: config.BaseJobConfig{
				Name:    "boot-bad-log-job",
				Command: "true",
				CanFail: canFail,
				Stdout:  filepath.Join(parent, "boot.log"),
			},
		})
		require.NoError(t, err, "construction must not open the log target")
		return job
	}

	require.NoError(t, newJob(true).Run(context.Background()),
		"canFail must rescue the failing open")
	require.Error(t, newJob(false).Run(context.Background()))

	// the failed open left job.stdout/job.stderr pointing at the process-wide
	// streams; closeStdFiles must not have closed those
	_, err := os.Stdout.Stat()
	require.NoError(t, err)
	_, err = os.Stderr.Stat()
	require.NoError(t, err)
}

// An unset timestampFormat is the documented default (RFC3339) and must not
// trigger the unknown-format warning — or any other log line above debug
// level, since the resolution runs on every job (re)start.
func TestResolveTimestampLayoutDefaultsToRFC3339WithoutWarning(t *testing.T) {
	logHook := logtest.NewGlobal()
	defer logHook.Reset()

	job := &baseJob{Config: &config.BaseJobConfig{Name: "default-format-job"}}
	layout := job.resolveTimestampLayout(log.WithField("job.name", job.Config.Name))

	require.Equal(t, time.RFC3339, layout)
	for _, entry := range logHook.AllEntries() {
		require.GreaterOrEqual(t, entry.Level, log.DebugLevel,
			"layout resolution must not log above debug: %s", entry.Message)
	}
}

// The chosen layout is logged at debug level only, so restart loops do not
// spam the info log with one layout line per start.
func TestResolveTimestampLayoutLogsLayoutAtDebugOnly(t *testing.T) {
	logHook := logtest.NewGlobal()
	defer logHook.Reset()

	previousLevel := log.GetLevel()
	log.SetLevel(log.DebugLevel)
	t.Cleanup(func() { log.SetLevel(previousLevel) })

	job := &baseJob{Config: &config.BaseJobConfig{
		Name:            "debug-layout-job",
		TimestampFormat: "Kitchen",
	}}
	job.resolveTimestampLayout(log.WithField("job.name", job.Config.Name))

	var messages []string
	for _, entry := range logHook.AllEntries() {
		require.Equal(t, log.DebugLevel, entry.Level, "unexpected level for: %s", entry.Message)
		messages = append(messages, entry.Message)
	}
	require.Contains(t, messages, "logging with timestamp layout")
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
	// the layout contains no time components, so the expected output is
	// deterministic while the timestamp code path is still exercised
	job.forwardOutput(io.NopCloser(strings.NewReader(input)), &buf, "T", []byte("[long-line-job] "))

	prefix := "[T] [long-line-job] "
	require.Equal(t,
		prefix+"first\n"+prefix+payload+"\n"+prefix+"\n"+prefix+"last\n",
		buf.String())

	for _, entry := range logHook.AllEntries() {
		require.NotEqual(t, "error reading from process", entry.Message)
	}
}

// A job with decoration explicitly disabled — the opt-out state after
// ApplyJobLogDefaults materialized false onto it — keeps the raw fd
// passthrough. The output deliberately has no trailing newline: the line
// forwarder would append one, so byte-identical output here proves nothing
// was piped through mittnite, not just that the decoration was empty.
func TestStartOnceRawPassthroughWhenBothOptionsExplicitlyDisabled(t *testing.T) {
	stdoutPath := filepath.Join(t.TempDir(), "stdout.log")

	job := &baseJob{}
	job.init(&config.BaseJobConfig{
		Name:             "raw-job",
		Command:          "printf",
		Args:             []string{"hello"},
		EnableTimestamps: boolPtr(false),
		EnableNamePrefix: boolPtr(false),
		Stdout:           stdoutPath,
	})

	require.NoError(t, job.startOnce(context.Background(), nil))

	content, err := os.ReadFile(stdoutPath)
	require.NoError(t, err)
	require.Equal(t, "hello", string(content))
}

// ApplyJobLogDefaults composed with the job constructors and startOnce: jobs
// and boot jobs without explicit log options pick up the flipped global
// defaults and emit fully decorated output. (The cmd/up wiring that passes
// the flag values — after config generation — is covered by E2E runs, not
// here.)
func TestDefaultDecorationEndToEnd(t *testing.T) {
	dir := t.TempDir()
	jobOut := filepath.Join(dir, "job.log")
	bootOut := filepath.Join(dir, "boot.log")

	ignition := &config.Ignition{
		Jobs: []config.JobConfig{{
			BaseJobConfig: config.BaseJobConfig{
				Name:    "default-job",
				Command: "echo",
				Args:    []string{"hello"},
				Stdout:  jobOut,
			},
		}},
		BootJobs: []config.BootJobConfig{{
			BaseJobConfig: config.BaseJobConfig{
				Name:    "default-boot",
				Command: "echo",
				Args:    []string{"ahoi"},
				Stdout:  bootOut,
			},
		}},
	}
	ignition.ApplyJobLogDefaults(true, true)

	commonJob, err := NewCommonJob(&ignition.Jobs[0])
	require.NoError(t, err)
	require.NoError(t, commonJob.startOnce(context.Background(), nil))

	bootJob, err := NewBootJob(&ignition.BootJobs[0])
	require.NoError(t, err)
	require.NoError(t, bootJob.Run(context.Background()))

	pattern := `^\[\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(Z|[+-]\d{2}:\d{2})\] \[%s\] %s\n$`
	content, err := os.ReadFile(jobOut)
	require.NoError(t, err)
	require.Regexp(t, fmt.Sprintf(pattern, "default-job", "hello"), string(content))

	content, err = os.ReadFile(bootOut)
	require.NoError(t, err)
	require.Regexp(t, fmt.Sprintf(pattern, "default-boot", "ahoi"), string(content))
}

// flakyWriter fails or passes each write according to the per-call results
// list (nil = success); writes beyond the list succeed.
type flakyWriter struct {
	results []error
	buf     bytes.Buffer
	calls   int
}

func (w *flakyWriter) Write(p []byte) (int, error) {
	defer func() { w.calls++ }()
	if w.calls < len(w.results) && w.results[w.calls] != nil {
		return 0, w.results[w.calls]
	}
	w.buf.Write(p)
	return len(p), nil
}

// A persistently failing log target must not be logged at the child's write
// rate: one error per failure streak, and forwarding resumes when the target
// recovers.
func TestForwardOutputLogsPersistentWriteErrorsOncePerStreak(t *testing.T) {
	logHook := logtest.NewGlobal()
	defer logHook.Reset()

	brokenTarget := fmt.Errorf("target broken")
	w := &flakyWriter{results: []error{brokenTarget, brokenTarget, nil, brokenTarget, brokenTarget}}

	job := &baseJob{Config: &config.BaseJobConfig{Name: "flaky-target-job"}}
	job.forwardOutput(io.NopCloser(strings.NewReader("one\ntwo\nthree\nfour\nfive\n")),
		w, "", []byte("[flaky-target-job] "))

	errorCount := 0
	for _, entry := range logHook.AllEntries() {
		if entry.Level == log.ErrorLevel {
			errorCount++
		}
	}
	require.Equal(t, 2, errorCount, "expected one error per failure streak")
	require.Equal(t, "[flaky-target-job] three\n", w.buf.String(),
		"writes must still be attempted after failures")
}

// When the flush wait times out because a lingering child keeps the pipes
// open, the forwarder goroutines outlive startOnce; a restart then reassigns
// job.stdout/job.stderr via CreateAndOpenStdFile, so the forwarders must have
// captured their targets with a proper happens-before edge (only fails under
// -race).
func TestStartOnceRestartDoesNotRaceWithLingeringForwarders(t *testing.T) {
	job := &baseJob{}
	job.init(&config.BaseJobConfig{
		Name:    "restart-race-job",
		Command: "sh",
		// the child ignores TERM and holds the inherited pipe write ends open
		// well past the flush wait without writing, so the forwarders never
		// see EOF before the restart; the main process sleeps briefly so the
		// trap is installed before startOnce signals the process group
		Args:             []string{"-c", "(trap '' TERM; sleep 3) & sleep 0.2"},
		EnableTimestamps: boolPtr(true),
		Stdout:           filepath.Join(t.TempDir(), "stdout.log"),
	})

	start := time.Now()
	require.NoError(t, job.startOnce(context.Background(), nil))
	// immediate restart, like CommonJob.Run does after ProcessWillBeRestartedError
	require.NoError(t, job.startOnce(context.Background(), nil))

	// the flush wait is capped at one second per start; an unbounded wait
	// would block on each child's 3s pipe hold and take over six seconds
	require.Less(t, time.Since(start), 4500*time.Millisecond)
}

// With a file-backed log target, startOnce waits for the forwarders to drain
// the job's own final output into the file before closeStdFiles closes it;
// the last lines of a job must not race into the discard path.
func TestStartOnceFlushesFileTargetBeforeReturning(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "stdout.log")

	job := &baseJob{}
	job.init(&config.BaseJobConfig{
		Name:             "flush-job",
		Command:          "sh",
		Args:             []string{"-c", "echo final-line"},
		EnableTimestamps: boolPtr(true),
		EnableNamePrefix: boolPtr(true),
		Stdout:           logFile,
	})

	require.NoError(t, job.startOnce(context.Background(), nil))

	// deliberately no Eventually: the file must be complete when startOnce
	// has returned
	content, err := os.ReadFile(logFile)
	require.NoError(t, err)
	require.Regexp(t, `^\[\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(Z|[+-]\d{2}:\d{2})\] \[flush-job\] final-line\n$`, string(content))
}

func TestStartOncePrefixesOutputWithTimestampAndJobName(t *testing.T) {
	job := &baseJob{}
	job.init(&config.BaseJobConfig{
		Name:             "prefix-job",
		Command:          "sh",
		Args:             []string{"-c", "echo hello"},
		EnableTimestamps: boolPtr(true),
		EnableNamePrefix: boolPtr(true),
	})

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
	job.init(&config.BaseJobConfig{
		Name:             "name-only-job",
		Command:          "sh",
		Args:             []string{"-c", "echo hello"},
		EnableNamePrefix: boolPtr(true),
	})

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
