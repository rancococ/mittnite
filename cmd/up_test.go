package cmd

import (
	"os"
	"testing"

	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/require"
)

func TestEnvBool(t *testing.T) {
	parsable := map[string]bool{
		"1":     true,
		"true":  true,
		"TRUE":  true,
		"t":     true,
		"0":     false,
		"false": false,
	}
	fallsBack := []string{
		"",
		"yes", // not a strconv.ParseBool value
	}

	for _, defaultValue := range []bool{true, false} {
		for value, expected := range parsable {
			t.Setenv("MITTNITE_ENVBOOL_TEST", value)
			require.Equal(t, expected, envBool("MITTNITE_ENVBOOL_TEST", defaultValue),
				"value %q, default %t", value, defaultValue)
		}
		for _, value := range fallsBack {
			t.Setenv("MITTNITE_ENVBOOL_TEST", value)
			require.Equal(t, defaultValue, envBool("MITTNITE_ENVBOOL_TEST", defaultValue),
				"value %q must fall back to the default", value)
		}
		require.Equal(t, defaultValue, envBool("MITTNITE_ENVBOOL_TEST_UNSET", defaultValue),
			"unset must fall back to the default")
	}
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
	require.Contains(t, warnings[0], "using default true",
		"the warning must state the assumed value, since unparsable now means on")
}

// Job output decoration is on by default since v2.0.0. The flag defaults are
// fixed at package init from the environment, so this only asserts the
// built-in default when the variables are absent from the test process.
func TestJobLogFlagDefaultsAreTrue(t *testing.T) {
	for _, key := range []string{envJobLogTimestamps, envJobLogNamePrefix} {
		if _, ok := os.LookupEnv(key); ok {
			t.Skipf("%s is set; flag defaults were derived from it at package init", key)
		}
	}

	for _, name := range []string{"job-log-timestamps", "job-log-name-prefix"} {
		flag := up.PersistentFlags().Lookup(name)
		require.NotNil(t, flag)
		require.Equal(t, "true", flag.DefValue, "--%s must default to on", name)
	}
}
