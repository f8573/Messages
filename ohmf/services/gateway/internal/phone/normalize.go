package phone

import (
	"strings"
	"unicode"
)

// NormalizeE164 converts common phone-number input forms into a strict E.164
// representation and rejects values that cannot be normalized safely.
func NormalizeE164(v string) string {
	raw := strings.TrimSpace(v)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "00") {
		raw = "+" + strings.TrimPrefix(raw, "00")
	}

	var b strings.Builder
	for i, r := range raw {
		switch {
		case r == '+' && i == 0:
			b.WriteRune(r)
		case unicode.IsDigit(r):
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '(' || r == ')' || r == '.':
			continue
		default:
			return ""
		}
	}

	normalized := b.String()
	if !strings.HasPrefix(normalized, "+") {
		return ""
	}
	digits := normalized[1:]
	if len(digits) < 8 || len(digits) > 15 {
		return ""
	}
	if digits[0] == '0' {
		return ""
	}
	for _, r := range digits {
		if !unicode.IsDigit(r) {
			return ""
		}
	}
	return normalized
}
