package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The bare-mittnite fallback must delegate to up's RunE: up defines no Run,
// so a Run-based delegation calls a nil function — exactly the crash the v2
// fallback repair removed.
func TestRootFallbackDelegatesToUpRunE(t *testing.T) {
	require.Nil(t, up.Run, "up switched to RunE in cdb2ecf; a Run delegation would be a nil call")
	require.NotNil(t, up.RunE)
	require.Nil(t, rootCmd.Run, "the fallback must use RunE, matching up")
	require.NotNil(t, rootCmd.RunE)
}
