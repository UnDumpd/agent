package s3

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestIsOldEnough(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name   string
		latest time.Time
		minAge time.Duration
		want   bool
	}{
		{
			name:   "latest well before now minus min age",
			latest: now.Add(-10 * time.Minute),
			minAge: 5 * time.Minute,
			want:   true,
		},
		{
			name:   "latest exactly min age before now is inclusive boundary",
			latest: now.Add(-5 * time.Minute),
			minAge: 5 * time.Minute,
			want:   true,
		},
		{
			name:   "latest too recent",
			latest: now.Add(-1 * time.Minute),
			minAge: 5 * time.Minute,
			want:   false,
		},
		{
			name:   "zero min age disables the guard",
			latest: now,
			minAge: 0,
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isOldEnough(tt.latest, now, tt.minAge))
		})
	}
}
