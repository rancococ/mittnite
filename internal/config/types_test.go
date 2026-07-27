package config

import (
	"testing"

	"github.com/hashicorp/hcl"
	"github.com/stretchr/testify/require"
)

// The log options are tri-state: HCL must distinguish an unset option (nil)
// from an explicit false, so that ApplyJobLogDefaults only fills the gaps.
func TestHCLKeepsUnsetLogOptionsDistinctFromFalse(t *testing.T) {
	src := `
job "unset" {
  command = "true"
}

job "opt-out" {
  command = "true"
  enableTimestamps = false
  enableNamePrefix = false
}

job "opt-in" {
  command = "true"
  enableTimestamps = true
  enableNamePrefix = true
}
`

	ign := &Ignition{}
	require.NoError(t, hcl.Unmarshal([]byte(src), ign))
	require.Len(t, ign.Jobs, 3)

	require.Nil(t, ign.Jobs[0].EnableTimestamps)
	require.Nil(t, ign.Jobs[0].EnableNamePrefix)

	require.NotNil(t, ign.Jobs[1].EnableTimestamps)
	require.False(t, *ign.Jobs[1].EnableTimestamps)
	require.NotNil(t, ign.Jobs[1].EnableNamePrefix)
	require.False(t, *ign.Jobs[1].EnableNamePrefix)

	require.NotNil(t, ign.Jobs[2].EnableTimestamps)
	require.True(t, *ign.Jobs[2].EnableTimestamps)
	require.NotNil(t, ign.Jobs[2].EnableNamePrefix)
	require.True(t, *ign.Jobs[2].EnableNamePrefix)
}

func TestApplyJobLogDefaultsFillsOnlyUnsetOptions(t *testing.T) {
	optOut := false

	ign := &Ignition{
		Jobs: []JobConfig{
			{BaseJobConfig: BaseJobConfig{Name: "unset"}},
			{BaseJobConfig: BaseJobConfig{
				Name:             "opt-out",
				EnableTimestamps: &optOut,
				EnableNamePrefix: &optOut,
			}},
		},
		BootJobs: []BootJobConfig{
			{BaseJobConfig: BaseJobConfig{Name: "boot-unset"}},
		},
	}

	ign.ApplyJobLogDefaults(true, true)

	require.True(t, ign.Jobs[0].TimestampsEnabled())
	require.True(t, ign.Jobs[0].NamePrefixEnabled())
	require.False(t, ign.Jobs[1].TimestampsEnabled())
	require.False(t, ign.Jobs[1].NamePrefixEnabled())
	require.True(t, ign.BootJobs[0].TimestampsEnabled())
	require.True(t, ign.BootJobs[0].NamePrefixEnabled())
}

func TestApplyJobLogDefaultsOffKeepsExplicitOptIn(t *testing.T) {
	optIn := true

	ign := &Ignition{
		Jobs: []JobConfig{
			{BaseJobConfig: BaseJobConfig{Name: "unset"}},
			{BaseJobConfig: BaseJobConfig{
				Name:             "opt-in",
				EnableTimestamps: &optIn,
				EnableNamePrefix: &optIn,
			}},
		},
	}

	ign.ApplyJobLogDefaults(false, false)

	// the accessors also return false for nil, so pin that the "off" default
	// is materialized as an explicit false (visible in the job status API)
	require.NotNil(t, ign.Jobs[0].EnableTimestamps)
	require.NotNil(t, ign.Jobs[0].EnableNamePrefix)
	require.False(t, ign.Jobs[0].TimestampsEnabled())
	require.False(t, ign.Jobs[0].NamePrefixEnabled())
	require.True(t, ign.Jobs[1].TimestampsEnabled())
	require.True(t, ign.Jobs[1].NamePrefixEnabled())
}
