package journey

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

// IsInQuietHours checks if now is inside quiet hours in recipient local time.
// If it is, it returns true and the next available UTC time when quiet hours end.
func IsInQuietHours(now time.Time, profile domain.Profile, quietHoursStart, quietHoursEnd *int, defaultTimezone string) (bool, time.Time, error) {
	if quietHoursStart == nil || quietHoursEnd == nil {
		return false, now, nil
	}

	start := *quietHoursStart
	end := *quietHoursEnd
	if start == end {
		return false, now, errors.New("quiet hours start and end must differ")
	}

	// Resolve timezone from profile or tenant fallback
	tz := defaultTimezone
	if tz == "" {
		tz = "UTC"
	}

	var attrs map[string]any
	if len(profile.Attributes) > 0 {
		_ = json.Unmarshal(profile.Attributes, &attrs)
		if attrs != nil {
			if t, ok := attrs["timezone"].(string); ok && t != "" {
				tz = t
			}
		}
	}

	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}

	localTime := now.In(loc)
	currentHour := localTime.Hour()

	inQuietHours := false
	var nextOpenLocal time.Time

	if start < end {
		// Same calendar day, e.g. 13:00 to 15:00
		if currentHour >= start && currentHour < end {
			inQuietHours = true
			nextOpenLocal = time.Date(localTime.Year(), localTime.Month(), localTime.Day(), end, 0, 0, 0, loc)
		}
	} else {
		// Wraps around midnight, e.g. 22:00 to 08:00
		if currentHour >= start || currentHour < end {
			inQuietHours = true
			if currentHour >= start {
				// We are in the evening, next open is tomorrow at "end" hour
				tomorrow := localTime.AddDate(0, 0, 1)
				nextOpenLocal = time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), end, 0, 0, 0, loc)
			} else {
				// We are in the morning, next open is today at "end" hour
				nextOpenLocal = time.Date(localTime.Year(), localTime.Month(), localTime.Day(), end, 0, 0, 0, loc)
			}
		}
	}

	if inQuietHours {
		return true, nextOpenLocal.UTC(), nil
	}

	return false, now, nil
}
