package middleware

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"ohmf/services/gateway/internal/config"
	"ohmf/services/gateway/internal/version"
)

// APIVersionMiddleware returns a chi middleware that sets API and spec version
// response headers, performs basic Accept-Version negotiation and semantic-
// major compatibility checks, and emits optional Deprecation/Sunset headers.
func APIVersionMiddleware(cfg config.Config) func(http.Handler) http.Handler {
	// derive a semver for the server API (e.g. v1 -> 1.0.0)
	sv := strings.TrimPrefix(version.APIVersion, "v")
	if !strings.Contains(sv, ".") {
		sv = sv + ".0.0"
	}
	serverVer, _ := semver.NewVersion(sv)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// always expose server API and spec versions
			w.Header().Set("X-OHMF-API-Version", version.APIVersion)
			w.Header().Set("X-OHMF-Spec-Version", version.SpecVersion)

			// emit optional deprecation/sunset headers from config
			if cfg.APIDeprecation != "" {
				w.Header().Set("Deprecation", cfg.APIDeprecation)
			}
			if cfg.APISunset != "" {
				w.Header().Set("Sunset", cfg.APISunset)
			}

			// client may send desired API in header X-OHMF-Client-API (e.g. v1)
			client := r.Header.Get("X-OHMF-Client-API")
			if client != "" {
				clientMajor := parseMajor(client)
				if clientMajor >= 0 && clientMajor != int(serverVer.Major()) {
					// warn consumers: incompatible major version
					w.Header().Add("Warning", "199 - \"client API version incompatible\"")
				}
			}

			// Honor Accept-Version header: support semver constraints. If the
			// client requires a version that doesn't match the server's API,
			// return 406 Not Acceptable. Accept-Version may contain
			// comma-separated semver constraints; any matching constraint
			// accepts the request.
			if av := r.Header.Get("Accept-Version"); av != "" {
				ok := false
				parts := strings.Split(av, ",")
				for _, p := range parts {
					p = strings.TrimSpace(p)
					if p == "" {
						continue
					}
					if p == "*" {
						ok = true
						break
					}
					// Try parse as a semver constraint
					if c, err := semver.NewConstraint(p); err == nil {
						if c.Check(serverVer) {
							ok = true
							break
						}
						continue
					}
					// Fallback: if token looks like a short major (e.g. v1 or 1),
					// convert to semver and compare major equality.
					tp := strings.TrimPrefix(p, "v")
					if !strings.Contains(tp, ".") {
						tp = tp + ".0.0"
					}
					if v, err := semver.NewVersion(tp); err == nil {
						if v.Major() == serverVer.Major() {
							ok = true
							break
						}
					}
				}
				if !ok {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusNotAcceptable)
					_ = json.NewEncoder(w).Encode(map[string]string{
						"error":   "client requested unacceptable API version",
						"message": "server supports " + version.APIVersion,
					})
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

func parseMajor(v string) int {
	v = strings.TrimSpace(v)
	if v == "" {
		return -1
	}
	// remove leading 'v' or 'V'
	if v[0] == 'v' || v[0] == 'V' {
		v = v[1:]
	}
	// take up to first dot
	if i := strings.IndexByte(v, '.'); i >= 0 {
		v = v[:i]
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return -1
	}
	return n
}
