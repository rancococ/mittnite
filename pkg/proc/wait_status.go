package proc

import (
	"errors"
	"os/exec"
	"syscall"

	log "github.com/sirupsen/logrus"
)

// decodeWaitStatus renders a raw wait status as readable log fields:
// exit.code for regular exits, exit.signal (plus core.dumped) for
// signal-terminated processes.
func decodeWaitStatus(s syscall.WaitStatus) log.Fields {
	switch {
	case s.Exited():
		return log.Fields{"exit.code": s.ExitStatus()}
	case s.Signaled():
		fields := log.Fields{"exit.signal": s.Signal().String()}
		if s.CoreDump() {
			fields["core.dumped"] = true
		}
		return fields
	default:
		return log.Fields{"exit.status": int(s)}
	}
}

// terminatedBySignal reports whether err is an exec.ExitError caused by the
// process dying from the given signal.
func terminatedBySignal(err error, sig syscall.Signal) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	ws, ok := exitErr.Sys().(syscall.WaitStatus)
	return ok && ws.Signaled() && ws.Signal() == sig
}
