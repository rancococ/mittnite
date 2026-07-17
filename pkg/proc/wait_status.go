package proc

import (
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
