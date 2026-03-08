package limit

import (
	"net/http"
	"strconv"
)

func SetHeaders(w http.ResponseWriter, limit int64, decision Decision) {
	w.Header().Set("X-RateLimit-Limit", strconv.FormatInt(limit, 10))
	w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(decision.Remaining, 10))
	if decision.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(decision.RetryAfter.Seconds())+1))
		w.Header().Set("X-RateLimit-Retry-After-Ms", strconv.FormatInt(decision.RetryAfter.Milliseconds(), 10))
	}
}
