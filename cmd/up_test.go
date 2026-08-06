package cmd

import (
	"testing"

	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/require"
)

func TestEnvBool(t *testing.T) {
	cases := map[string]bool{
		"1":     true,
		"true":  true,
		"TRUE":  true,
		"t":     true,
		"0":     false,
		"false": false,
		"":      false,
		"yes":   false, // not a strconv.ParseBool value, counts as false
	}

	for value, expected := range cases {
		t.Setenv("MITTNITE_ENVBOOL_TEST", value)
		require.Equal(t, expected, envBool("MITTNITE_ENVBOOL_TEST"), "value %q", value)
	}

	require.False(t, envBool("MITTNITE_ENVBOOL_TEST_UNSET"))
}

func TestWarnUnparsableEnvBools(t *testing.T) {
	logHook := logtest.NewGlobal()
	defer logHook.Reset()

	t.Setenv(envJobLogTimestamps, "yes")
	t.Setenv(envJobLogNamePrefix, "true")

	warnUnparsableEnvBools()

	var warnings []string
	for _, entry := range logHook.AllEntries() {
		if entry.Level == log.WarnLevel {
			warnings = append(warnings, entry.Message)
		}
	}
	require.Len(t, warnings, 1, "only the unparsable variable should be warned about")
	require.Contains(t, warnings[0], envJobLogTimestamps)
}
