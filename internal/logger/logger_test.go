package logger

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_LevelMapping(t *testing.T) {
	cases := []struct {
		in    string
		want  slog.Level
		extra string
	}{
		{"debug", slog.LevelDebug, ""},
		{"DEBUG", slog.LevelDebug, "case-insensitive"},
		{"info", slog.LevelInfo, ""},
		{"warn", slog.LevelWarn, ""},
		{"warning", slog.LevelWarn, "warning alias"},
		{"error", slog.LevelError, ""},
		{"", slog.LevelInfo, "fallback on empty"},
		{"nonsense", slog.LevelInfo, "fallback on unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.in+tc.extra, func(t *testing.T) {
			l := New(tc.in)
			require.NotNil(t, l)
			// Lowest-enabled probe — `Enabled` is level-based.
			assert.True(t, l.Enabled(context.Background(), tc.want),
				"level %s must be enabled for input %q", tc.want, tc.in)
			if tc.want > slog.LevelDebug {
				assert.False(t, l.Enabled(context.Background(), tc.want-1),
					"levels below %s must NOT be enabled for input %q", tc.want, tc.in)
			}
		})
	}
}
