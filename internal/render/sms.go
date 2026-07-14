package render

// IsGSM7 returns true if the rune is in the basic or extended GSM 7-bit character set.
func IsGSM7(r rune) bool {
	switch r {
	case '@', '£', '$', '¥', 'è', 'é', 'ù', 'ì', 'ò', 'Ç', '\n', 'Ø', 'ø', '\r', 'Å', 'å',
		'Δ', '_', 'Φ', 'Γ', 'Λ', 'Ω', 'Π', 'Ψ', 'Σ', 'Θ', 'Ξ', 'Æ', 'æ', 'ß', 'É',
		' ', '!', '"', '#', '¤', '%', '&', '\'', '(', ')', '*', '+', ',', '-', '.', '/',
		'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', ':', ';', '<', '=', '>', '?',
		'¡', 'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K', 'L', 'M', 'N', 'O', 'P', 'Q', 'R', 'S', 'T', 'U', 'V', 'W', 'X', 'Y', 'Z',
		'Ä', 'Ö', 'Ñ', 'Ü', '§', '¿',
		'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm', 'n', 'o', 'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z',
		'ä', 'ö', 'ñ', 'ü', 'à':
		return true
	case '[', '\\', ']', '^', '{', '|', '}', '~', '€':
		return true
	}
	return false
}

// IsGSM7Extended returns true if the rune belongs to the extended GSM-7 character set
// and thus occupies 2 character units in the encoded SMS.
func IsGSM7Extended(r rune) bool {
	switch r {
	case '[', '\\', ']', '^', '{', '|', '}', '~', '€':
		return true
	}
	return false
}

// AnalyzeSMS analyzes the text body of an SMS and calculates its encoding type,
// encoded character count, and the number of message segments.
func AnalyzeSMS(body string) (isUCS2 bool, charCount int, segments int) {
	isUCS2 = false
	charCount = 0

	for _, r := range body {
		if !IsGSM7(r) {
			isUCS2 = true
		}
	}

	if isUCS2 {
		// UCS-2 encoding: each character (rune) counts as 1.
		// segment limit:
		// - 1 segment: up to 70 characters.
		// - Multiple segments: 67 characters per segment.
		runes := []rune(body)
		charCount = len(runes)
		if charCount <= 70 {
			segments = 1
		} else {
			segments = (charCount + 66) / 67
		}
	} else {
		// GSM-7 encoding: basic characters count as 1, extended characters count as 2.
		for _, r := range body {
			if IsGSM7Extended(r) {
				charCount += 2
			} else {
				charCount += 1
			}
		}
		// GSM-7 segment limit:
		// - 1 segment: up to 160 characters.
		// - Multiple segments: 153 characters per segment.
		if charCount <= 160 {
			segments = 1
		} else {
			segments = (charCount + 152) / 153
		}
	}

	if segments == 0 && len(body) > 0 {
		segments = 1
	}
	return isUCS2, charCount, segments
}
