package phone

import "testing"

func TestNormalizeE164(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"+1 (555) 123-4567", "+15551234567"},
		{"00447911123456", "+447911123456"},
		{"+491234567890", "+491234567890"},
		{"5551234567", ""},
		{"+0123456789", ""},
		{"", ""},
	}

	for _, tc := range tests {
		if got := NormalizeE164(tc.in); got != tc.want {
			t.Fatalf("NormalizeE164(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
