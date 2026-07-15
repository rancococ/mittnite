package proc

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestManagedPidRegistry(t *testing.T) {
	registerManagedPid(1234567, "some-job")

	jobName, ok := lookupManagedPid(1234567)
	assert.True(t, ok)
	assert.Equal(t, "some-job", jobName)

	unregisterManagedPid(1234567)

	_, ok = lookupManagedPid(1234567)
	assert.False(t, ok)
}
