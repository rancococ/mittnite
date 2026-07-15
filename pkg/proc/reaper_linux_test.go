//go:build linux && (amd64 || arm64)

package proc

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PR_SET_CHILD_SUBREAPER; makes the test process adopt orphaned descendants
// the same way PID 1 does
const prSetChildSubreaper = 36

func becomeSubreaper(t *testing.T) {
	t.Helper()
	if _, _, errno := syscall.Syscall(syscall.SYS_PRCTL, prSetChildSubreaper, 1, 0); errno != 0 {
		t.Skipf("cannot become child subreaper: %v", errno)
	}
}

func TestReapZombiesCollectsAndAttributesOrphans(t *testing.T) {
	becomeSubreaper(t)

	logHook := logtest.NewGlobal()
	defer logHook.Reset()

	// the shell spawns a background child and exits immediately; the child is
	// orphaned and re-parented to this (subreaper) process
	cmd := exec.Command("sh", "-c", "sleep 0.3 >/dev/null 2>&1 & echo $!")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, err := cmd.Output()
	require.NoError(t, err)

	orphanPid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	require.NoError(t, err)

	// the orphan kept the shell's process group, so it must be attributed to
	// the job registered under the shell's pid
	registerManagedPid(cmd.Process.Pid, "test-job")
	defer unregisterManagedPid(cmd.Process.Pid)

	require.Eventually(t, func() bool {
		reapZombies()
		// once reaped, the pid is gone entirely (kill fails with ESRCH)
		return syscall.Kill(orphanPid, 0) != nil
	}, 5*time.Second, 50*time.Millisecond, "orphan should be reaped")

	for _, entry := range logHook.AllEntries() {
		if entry.Message == "reaped orphaned process" && entry.Data["pid"] == orphanPid {
			assert.Equal(t, "test-job", entry.Data["job.name"])
			assert.Equal(t, 0, entry.Data["exit.code"])
			return
		}
	}
	t.Fatalf("no reap log entry found for orphan pid %d", orphanPid)
}

func TestReapZombiesLeavesManagedProcessesToSupervisor(t *testing.T) {
	becomeSubreaper(t)

	cmd := exec.Command("sh", "-c", "exit 3")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, cmd.Start())

	pid := cmd.Process.Pid
	registerManagedPid(pid, "managed-job")
	defer unregisterManagedPid(pid)

	// let the process exit and turn into a zombie without waiting on it
	require.Eventually(t, func() bool {
		z, err := peekZombie()
		return err == nil && z == pid
	}, 5*time.Second, 20*time.Millisecond, "managed process should become a zombie")

	// any number of reap passes must not consume the zombie
	for i := 0; i < 10; i++ {
		reapZombies()
	}

	// the exit status must still be available to the supervisor's Wait
	err := cmd.Wait()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "Wait must see the real exit status, not ECHILD")
	assert.Equal(t, 3, exitErr.ExitCode())
}
