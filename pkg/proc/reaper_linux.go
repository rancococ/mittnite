//go:build linux && (amd64 || arm64)

package proc

import (
	"os"
	"os/signal"
	"syscall"
	"time"
	"unsafe"

	log "github.com/sirupsen/logrus"
)

// siginfo mirrors the kernel's siginfo_t layout for child-exit events on
// the 64-bit platforms this file is built for (see build constraint; 32-bit
// layouts differ). Only the fields up to Status are read; the trailing
// padding brings the struct to the 128 bytes the kernel writes.
type siginfo struct {
	Signo  int32
	Errno  int32
	Code   int32
	_      int32
	Pid    int32
	Uid    uint32
	Status int32
	_      [100]byte
}

// idtype P_ALL for waitid(2); not exported by package syscall
const pAll = 0

// ReapZombies reaps orphaned child processes that get re-parented to
// mittnite when it runs as PID 1. Processes that belong to a managed job are
// deliberately left alone, so that their exit status stays available to the
// job supervisor's cmd.Wait().
func ReapZombies() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGCHLD)

	// SIGCHLD is coalesced while a reap pass is running; the ticker collects
	// zombies whose signal was lost that way
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case sig := <-signals:
			log.WithField("signal", sig).Debug("handling signal")
		case <-ticker.C:
		}

		reapZombies()
	}
}

func reapZombies() {
	for {
		pid, err := peekZombie()
		if err != nil {
			log.WithError(err).Warn("failed to check for zombie processes")
			return
		}
		if pid == 0 {
			return
		}

		if jobName, managed := lookupManagedPid(pid); managed {
			// the exit status of this process belongs to the job supervisor's
			// cmd.Wait(), which will reap it momentarily; zombies queued
			// behind it are collected on the next SIGCHLD or tick
			log.WithField("pid", pid).WithField("job.name", jobName).
				Debug("leaving exited process to its job supervisor")
			return
		}

		logger := log.WithField("pid", pid)

		// jobs run in their own process group (Setpgid), so an orphan that
		// kept its group can be attributed to the job it descends from
		if pgid, err := syscall.Getpgid(pid); err == nil {
			if jobName, ok := lookupManagedPid(pgid); ok {
				logger = logger.WithField("job.name", jobName)
			}
		}

		var status syscall.WaitStatus
		reaped, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
		if err != nil || reaped != pid {
			continue
		}

		logger.WithFields(decodeWaitStatus(status)).Info("reaped orphaned process")
	}
}

// peekZombie returns the pid of an exited child without reaping it, using
// waitid(2) with WNOWAIT. It returns 0 when no zombie is waiting.
func peekZombie() (int, error) {
	for {
		// zeroed on every attempt: with WNOHANG and no zombie pending, the
		// kernel leaves si_pid untouched
		var si siginfo

		_, _, errno := syscall.Syscall6(
			syscall.SYS_WAITID,
			pAll,
			0,
			uintptr(unsafe.Pointer(&si)),
			uintptr(syscall.WEXITED|syscall.WNOHANG|syscall.WNOWAIT),
			0, 0,
		)

		switch errno {
		case 0:
			return int(si.Pid), nil
		case syscall.EINTR:
			continue
		case syscall.ECHILD:
			return 0, nil
		default:
			return 0, errno
		}
	}
}
