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

func APIVersionMiddleware(cfg config.Config) func(http.Handler) http.Handler {
	sv := strings.TrimPrefix(version.APIVersion, "v")
	if !strings.Contains(sv, ".") {
		sv = sv + ".0.0"
	}
	serverVer, _ := semver.NewVersion(sv)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-OHMF-API-Version", version.APIVersion)
			w.Header().Set("X-OHMF-Spec-Version", version.SpecVersion)

			if cfg.APIDeprecation != "" {
				w.Header().Set("Deprecation", cfg.APIDeprecation)
			}
			if cfg.APISunset != "" {
				w.Header().Set("Sunset", cfg.APISunset)
			}

			client := r.Header.Get("X-OHMF-Client-API")
			if client != "" {
				clientMajor := parseMajor(client)
				if clientMajor >= 0 && clientMajor != int(serverVer.Major()) {
					w.Header().Add("Warning", "199 - \"client API version incompatible\"")
				}
			}

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
					if c, err := semver.NewConstraint(p); err == nil {
						if c.Check(serverVer) {
							ok = true
							break
						}
						continue
					}
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

// removed: redundant middleware narration stripped

func parseMajor(v string) int {
	v = strings.TrimSpace(v)
	if v == "" {
		return -1
	}
	if v[0] == 'v' || v[0] == 'V' {
		v = v[1:]
	}
	if i := strings.IndexByte(v, '.'); i >= 0 {
		v = v[:i]
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return -1
	}
	return n
}
