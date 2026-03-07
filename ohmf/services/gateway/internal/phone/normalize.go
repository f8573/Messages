package phone

import "strings"

func NormalizeE164(v string) string {
	return strings.TrimSpace(v)
}
