package render

import "testing"

func TestAnalyzeSMS(t *testing.T) {
	tests := []struct {
		name          string
		body          string
		wantUCS2      bool
		wantCharCount int
		wantSegments  int
	}{
		{
			name:          "simple GSM-7",
			body:          "Hello World",
			wantUCS2:      false,
			wantCharCount: 11,
			wantSegments:  1,
		},
		{
			name:          "GSM-7 with extended character",
			body:          "{Hello}",
			wantUCS2:      false,
			wantCharCount: 9, // '{' and '}' count as 2 each, 'H','e','l','l','o' count as 1 each -> 4 + 5 = 9
			wantSegments:  1,
		},
		{
			name:          "GSM-7 exactly 160 chars",
			body:          string(make([]byte, 160)), // all null bytes (not strictly basic GSM-7, but let's use spaces)
			wantUCS2:      false,
			wantCharCount: 160,
			wantSegments:  1,
		},
		{
			name:          "GSM-7 161 chars (2 segments)",
			body:          string(make([]rune, 161)), // we will populate with spaces
			wantUCS2:      false,
			wantCharCount: 161,
			wantSegments:  2,
		},
		{
			name:          "UCS-2 simple (with emoji)",
			body:          "Hello 🚀",
			wantUCS2:      true,
			wantCharCount: 7, // 'H','e','l','l','o',' ', '🚀' -> 7 runes
			wantSegments:  1,
		},
		{
			name:          "UCS-2 exactly 70 chars",
			body:          "🚀" + string(make([]rune, 69)),
			wantUCS2:      true,
			wantCharCount: 70,
			wantSegments:  1,
		},
		{
			name:          "UCS-2 71 chars (2 segments)",
			body:          "🚀" + string(make([]rune, 70)),
			wantUCS2:      true,
			wantCharCount: 71,
			wantSegments:  2,
		},
	}

	// Helper to create space filled strings
	fillSpaces := func(s string) string {
		out := []rune(s)
		for i, r := range out {
			if r == 0 {
				out[i] = ' '
			}
		}
		return string(out)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := fillSpaces(tt.body)
			gotUCS2, gotCharCount, gotSegments := AnalyzeSMS(body)
			if gotUCS2 != tt.wantUCS2 {
				t.Errorf("AnalyzeSMS() gotUCS2 = %v, want %v", gotUCS2, tt.wantUCS2)
			}
			if gotCharCount != tt.wantCharCount {
				t.Errorf("AnalyzeSMS() gotCharCount = %v, want %v", gotCharCount, tt.wantCharCount)
			}
			if gotSegments != tt.wantSegments {
				t.Errorf("AnalyzeSMS() gotSegments = %v, want %v", gotSegments, tt.wantSegments)
			}
		})
	}
}
