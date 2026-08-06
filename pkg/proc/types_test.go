package proc

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestTimeLayoutsCoverAllNamedGoTimeLayouts pins TimeLayouts to the full set
// of named layout constants in the time package, which is also the set the
// README's timestamp format table documents.
func TestTimeLayoutsCoverAllNamedGoTimeLayouts(t *testing.T) {
	require.Equal(t, map[string]string{
		"Layout":      time.Layout,
		"ANSIC":       time.ANSIC,
		"UnixDate":    time.UnixDate,
		"RubyDate":    time.RubyDate,
		"RFC822":      time.RFC822,
		"RFC822Z":     time.RFC822Z,
		"RFC850":      time.RFC850,
		"RFC1123":     time.RFC1123,
		"RFC1123Z":    time.RFC1123Z,
		"RFC3339":     time.RFC3339,
		"RFC3339Nano": time.RFC3339Nano,
		"Kitchen":     time.Kitchen,
		"Stamp":       time.Stamp,
		"StampMilli":  time.StampMilli,
		"StampMicro":  time.StampMicro,
		"StampNano":   time.StampNano,
		"DateTime":    time.DateTime,
		"DateOnly":    time.DateOnly,
		"TimeOnly":    time.TimeOnly,
	}, TimeLayouts)
}
