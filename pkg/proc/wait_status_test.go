package proc

import (
	"os/exec"
	"syscall"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func waitStatusOf(t *testing.T, command string) syscall.WaitStatus {
	t.Helper()
	cmd := exec.Command("sh", "-c", command)
	_ = cmd.Run()
	ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	require.True(t, ok, "process state should carry a wait status")
	return ws
}

func TestDecodeWaitStatusExitCodes(t *testing.T) {
	assert.Equal(t, log.Fields{"exit.code": 0}, decodeWaitStatus(waitStatusOf(t, "exit 0")))
	assert.Equal(t, log.Fields{"exit.code": 3}, decodeWaitStatus(waitStatusOf(t, "exit 3")))
}

func TestDecodeWaitStatusSignals(t *testing.T) {
	assert.Equal(t, log.Fields{"exit.signal": "terminated"}, decodeWaitStatus(waitStatusOf(t, "kill -TERM $$")))
	assert.Equal(t, log.Fields{"exit.signal": "hangup"}, decodeWaitStatus(waitStatusOf(t, "kill -HUP $$")))
}
