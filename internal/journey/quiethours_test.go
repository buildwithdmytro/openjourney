package journey

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

func ptrInt(i int) *int {
	return &i
}

func TestIsInQuietHours(t *testing.T) {
	// Base mock profile with different timezone attributes
	profileUTC := domain.Profile{
		Attributes: json.RawMessage(`{"timezone":"UTC"}`),
	}
	profileNY := domain.Profile{
		Attributes: json.RawMessage(`{"timezone":"America/New_York"}`),
	}
	profileNoTz := domain.Profile{
		Attributes: json.RawMessage(`{}`),
	}

	tests := []struct {
		name            string
		now             time.Time
		profile         domain.Profile
		start           *int
		end             *int
		defaultTz       string
		expectInQuiet   bool
		expectNextOpen  time.Time
	}{
		{
			name:            "No quiet hours configured",
			now:             time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC),
			profile:         profileUTC,
			start:           nil,
			end:             nil,
			defaultTz:       "UTC",
			expectInQuiet:   false,
			expectNextOpen:  time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC),
		},
		{
			name:            "Same day quiet hours - outside hours",
			now:             time.Date(2026, 7, 9, 11, 0, 0, 0, time.UTC),
			profile:         profileUTC,
			start:           ptrInt(13),
			end:             ptrInt(15),
			defaultTz:       "UTC",
			expectInQuiet:   false,
			expectNextOpen:  time.Date(2026, 7, 9, 11, 0, 0, 0, time.UTC),
		},
		{
			name:            "Same day quiet hours - inside hours",
			now:             time.Date(2026, 7, 9, 14, 30, 0, 0, time.UTC),
			profile:         profileUTC,
			start:           ptrInt(13),
			end:             ptrInt(15),
			defaultTz:       "UTC",
			expectInQuiet:   true,
			expectNextOpen:  time.Date(2026, 7, 9, 15, 0, 0, 0, time.UTC),
		},
		{
			name:            "Wrap around midnight - outside hours",
			now:             time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC),
			profile:         profileUTC,
			start:           ptrInt(22),
			end:             ptrInt(8),
			defaultTz:       "UTC",
			expectInQuiet:   false,
			expectNextOpen:  time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC),
		},
		{
			name:            "Wrap around midnight - inside hours (evening)",
			now:             time.Date(2026, 7, 9, 23, 15, 0, 0, time.UTC),
			profile:         profileUTC,
			start:           ptrInt(22),
			end:             ptrInt(8),
			defaultTz:       "UTC",
			expectInQuiet:   true,
			expectNextOpen:  time.Date(2026, 7, 10, 8, 0, 0, 0, time.UTC),
		},
		{
			name:            "Wrap around midnight - inside hours (morning)",
			now:             time.Date(2026, 7, 9, 5, 45, 0, 0, time.UTC),
			profile:         profileUTC,
			start:           ptrInt(22),
			end:             ptrInt(8),
			defaultTz:       "UTC",
			expectInQuiet:   true,
			expectNextOpen:  time.Date(2026, 7, 9, 8, 0, 0, 0, time.UTC),
		},
		{
			name:            "Profile timezone overrides default timezone - NY morning",
			// 10:00 UTC is 06:00 AM NY (Eastern Time)
			now:             time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC),
			profile:         profileNY,
			start:           ptrInt(22),
			end:             ptrInt(8),
			defaultTz:       "UTC",
			expectInQuiet:   true,
			// Ends at 08:00 AM NY (12:00 UTC)
			expectNextOpen:  time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC),
		},
		{
			name:            "Default timezone fallback used",
			// 10:00 UTC is 06:00 AM NY (Eastern Time)
			now:             time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC),
			profile:         profileNoTz,
			start:           ptrInt(22),
			end:             ptrInt(8),
			defaultTz:       "America/New_York",
			expectInQuiet:   true,
			expectNextOpen:  time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC),
		},
		{
			name:            "Invalid profile timezone falls back to UTC",
			now:             time.Date(2026, 7, 9, 5, 0, 0, 0, time.UTC),
			profile:         domain.Profile{Attributes: json.RawMessage(`{"timezone":"Invalid/Zone"}`)},
			start:           ptrInt(22),
			end:             ptrInt(8),
			defaultTz:       "UTC",
			expectInQuiet:   true,
			expectNextOpen:  time.Date(2026, 7, 9, 8, 0, 0, 0, time.UTC),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inQuiet, nextOpen, err := IsInQuietHours(tc.now, tc.profile, tc.start, tc.end, tc.defaultTz)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if inQuiet != tc.expectInQuiet {
				t.Errorf("expected inQuiet to be %v, got %v", tc.expectInQuiet, inQuiet)
			}

			if !nextOpen.Equal(tc.expectNextOpen) {
				t.Errorf("expected nextOpen to be %v, got %v", tc.expectNextOpen, nextOpen)
			}
		})
	}
}
