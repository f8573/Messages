package main

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/f8573/Messages/pkg/observability"
	"github.com/jackc/pgx/v5"
)

func currentUserID(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("X-User-ID"))
}

func isAdmin(r *http.Request) bool {
	return strings.ToLower(strings.TrimSpace(r.Header.Get("X-User-Role"))) == "admin"
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func errorJSON(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}

func decodeManifestPayload(r *http.Request) (map[string]any, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	if wrapped, ok := raw["manifest"]; ok {
		manifest, ok := wrapped.(map[string]any)
		if !ok {
			return nil, io.ErrUnexpectedEOF
		}
		return manifest, nil
	}
	return raw, nil
}

func developerModeEnabled(r *http.Request) bool {
	value := strings.TrimSpace(r.URL.Query().Get("developer_mode"))
	return value == "1" || strings.EqualFold(value, "true")
}

func parseLimitOffset(r *http.Request, defaultLimit int) (limit int, offset int) {
	limit = defaultLimit
	offset = 0
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			switch {
			case parsed <= 0:
				limit = defaultLimit
			case parsed > 100:
				limit = 100
			default:
				limit = parsed
			}
		}
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("cursor")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 0 {
			offset = parsed
		}
	}
	return limit, offset
}

func optionalBoolQuery(r *http.Request, key string) *bool {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	switch strings.ToLower(value) {
	case "1", "true", "yes":
		result := true
		return &result
	case "0", "false", "no":
		result := false
		return &result
	default:
		return nil
	}
}

func releaseVisibleToUser(app *registeredApp, release *appRelease, userID string, admin bool, developerMode bool) bool {
	if admin {
		return true
	}
	if release.SourceType == "dev" && !developerMode && app.OwnerUserID != userID && release.PublisherUserID != userID {
		return false
	}
	if release.ReviewStatus == statusApproved {
		return release.Visibility == "public" || app.OwnerUserID == userID || release.PublisherUserID == userID
	}
	return app.OwnerUserID == userID || release.PublisherUserID == userID
}

func latestVisibleRelease(app *registeredApp, userID string, admin bool, developerMode bool) *appRelease {
	var best *appRelease
	for _, release := range app.Releases {
		if !releaseVisibleToUser(app, release, userID, admin, developerMode) {
			continue
		}
		if best == nil || versionGreater(release.Version, best.Version) {
			best = release
		}
	}
	return best
}

func stringListContains(values []string, candidate string) bool {
	candidate = strings.TrimSpace(strings.ToLower(candidate))
	if candidate == "" {
		return true
	}
	for _, value := range values {
		if strings.ToLower(strings.TrimSpace(value)) == candidate {
			return true
		}
	}
	return false
}

func matchesCatalogFilters(app *registeredApp, release *appRelease, install *installedAppRef, r *http.Request, admin bool) bool {
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	if query != "" {
		joined := strings.ToLower(strings.Join([]string{app.AppID, app.Name, app.Summary, stringField(release.Manifest, "name")}, " "))
		if !strings.Contains(joined, query) {
			return false
		}
	}
	if reviewStatus := strings.TrimSpace(r.URL.Query().Get("review_status")); reviewStatus != "" {
		if !admin && app.OwnerUserID != currentUserID(r) && release.PublisherUserID != currentUserID(r) {
			return false
		}
		if release.ReviewStatus != reviewStatus {
			return false
		}
	}
	if sourceType := strings.TrimSpace(r.URL.Query().Get("source_type")); sourceType != "" && release.SourceType != sourceType {
		return false
	}
	if visibility := strings.TrimSpace(r.URL.Query().Get("visibility")); visibility != "" && release.Visibility != visibility {
		return false
	}
	if platform := strings.TrimSpace(r.URL.Query().Get("platform")); platform != "" && !stringListContains(release.SupportedPlatforms, platform) {
		return false
	}
	if owner := strings.TrimSpace(r.URL.Query().Get("owner_user_id")); owner != "" && app.OwnerUserID != owner {
		return false
	}
	if publisher := strings.TrimSpace(r.URL.Query().Get("publisher_user_id")); publisher != "" && release.PublisherUserID != publisher {
		return false
	}
	if installed := optionalBoolQuery(r, "installed"); installed != nil {
		if *installed && install == nil {
			return false
		}
		if !*installed && install != nil {
			return false
		}
	}
	return true
}

func (s *appsServer) registerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := currentUserID(r)
	if userID == "" {
		userID = "dev-bootstrap"
	}
	manifest, err := decodeManifestPayload(r)
	if err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid_request", "invalid manifest payload")
		return
	}
	if err := validateManifest(manifest); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid_manifest", err.Error())
		return
	}
	appID := stringField(manifest, "app_id")
	version := stringField(manifest, "version")
	createdStatus := http.StatusCreated
	state, err := s.mutateState(r.Context(), func(state *registryState) ([]auditEntry, error) {
		now := time.Now().UTC()
		manifestBytes, _ := json.Marshal(manifest)
		hash := manifestHash(manifestBytes)
		sourceType := manifestSourceType(manifest)
		app := state.Apps[appID]
		auditEntries := make([]auditEntry, 0, 2)
		if app == nil {
			app = &registeredApp{
				AppID:       appID,
				Name:        stringField(manifest, "name"),
				OwnerUserID: userID,
				Visibility:  manifestVisibility(manifest),
				Summary:     manifestSummary(manifest),
				CreatedAt:   now,
				UpdatedAt:   now,
				Releases:    map[string]*appRelease{},
			}
			state.Apps[appID] = app
			auditEntries = append(auditEntries, auditEntry{
				AppID:       appID,
				ActorUserID: userID,
				Action:      "app.created",
				CreatedAt:   now,
			})
		}
		if app.OwnerUserID != userID && !isAdmin(r) {
			return nil, &apiError{Status: http.StatusForbidden, Code: "forbidden", Message: "publisher does not own app"}
		}
		if existing := app.Releases[version]; existing != nil {
			if existing.ManifestHash != hash {
				return nil, &apiError{Status: http.StatusConflict, Code: "version_conflict", Message: "app_id/version already exists with different content"}
			}
			createdStatus = http.StatusOK
			return auditEntries, nil
		}
		// Compute manifest content hash for immutability enforcement
		manifestContentHash := computeManifestContentHash(manifestBytes)

		release := &appRelease{
			Version:             version,
			Manifest:            manifest,
			ManifestHash:        hash,
			ManifestContentHash: manifestContentHash,
			ReviewStatus:        initialReviewStatus(sourceType),
			SourceType:          sourceType,
			Visibility:          app.Visibility,
			PublisherUserID:     userID,
			SupportedPlatforms:  manifestPlatforms(manifest),
			EntrypointOrigin:    manifestOrigin(stringField(manifest["entrypoint"].(map[string]any), "url")),
			PreviewOrigin:       manifestOrigin(stringField(manifest["message_preview"].(map[string]any), "url")),
			CreatedAt:           now,
		}
		if release.ReviewStatus == statusApproved {
			release.PublishedAt = &now
			release.ReviewedAt = &now
		}
		app.Releases[version] = release
		app.Name = stringField(manifest, "name")
		app.Summary = manifestSummary(manifest)
		app.UpdatedAt = now
		refreshLatestPointers(app)
		auditEntries = append(auditEntries, auditEntry{
			AppID:       appID,
			Version:     version,
			ActorUserID: userID,
			Action:      "release.created",
			CreatedAt:   now,
			Metadata: map[string]any{
				"source_type":   sourceType,
				"review_status": release.ReviewStatus,
			},
		})
		return auditEntries, nil
	})
	if err != nil {
		respondRegistryError(w, err)
		return
	}
	app := state.Apps[appID]
	release := app.Releases[version]
	writeJSON(w, createdStatus, appResponse(app, release, userInstalls(state, currentUserID(r))[appID]))
}

func (s *appsServer) listAppsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	state, err := s.loadState()
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "load_failed", err.Error())
		return
	}
	userID := currentUserID(r)
	installs := userInstalls(state, userID)
	limit, offset := parseLimitOffset(r, 50)
	developerMode := developerModeEnabled(r)
	items := make([]map[string]any, 0, len(state.Apps))
	appIDs := make([]string, 0, len(state.Apps))
	for appID := range state.Apps {
		appIDs = append(appIDs, appID)
	}
	slices.Sort(appIDs)
	for _, appID := range appIDs {
		app := state.Apps[appID]
		release := latestVisibleRelease(app, userID, isAdmin(r), developerMode)
		if release == nil {
			continue
		}
		install := installs[app.AppID]
		if !matchesCatalogFilters(app, release, install, r, isAdmin(r)) {
			continue
		}
		items = append(items, appResponse(app, release, install))
	}
	total := len(items)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	nextCursor := ""
	if end < total {
		nextCursor = strconv.Itoa(end)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items[offset:end], "next_cursor": nextCursor, "total": total})
}

func (s *appsServer) getAppHandler(w http.ResponseWriter, r *http.Request, appID string) {
	state, err := s.loadState()
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "load_failed", err.Error())
		return
	}
	app := state.Apps[appID]
	if app == nil {
		errorJSON(w, http.StatusNotFound, "not_found", "app not found")
		return
	}
	release := latestVisibleRelease(app, currentUserID(r), isAdmin(r), developerModeEnabled(r))
	if release == nil {
		errorJSON(w, http.StatusNotFound, "not_found", "app not found")
		return
	}
	writeJSON(w, http.StatusOK, appResponse(app, release, userInstalls(state, currentUserID(r))[appID]))
}

func (s *appsServer) installAppHandler(w http.ResponseWriter, r *http.Request, appID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := currentUserID(r)
	if userID == "" {
		errorJSON(w, http.StatusUnauthorized, "unauthorized", "missing user header")
		return
	}
	var req struct {
		AcceptPermissionChanges bool `json:"accept_permission_changes"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	consentRequired := false
	state, err := s.mutateState(r.Context(), func(state *registryState) ([]auditEntry, error) {
		app := state.Apps[appID]
		if app == nil {
			return nil, &apiError{Status: http.StatusNotFound, Code: "not_found", Message: "app not found"}
		}
		release := latestApprovedRelease(app)
		if release == nil {
			return nil, &apiError{Status: http.StatusConflict, Code: "not_installable", Message: "app has no approved release"}
		}
		installs := userInstalls(state, userID)
		if existing := installs[appID]; existing != nil {
			if installedRelease := app.Releases[existing.InstalledVersion]; installedRelease != nil && existing.InstalledVersion != release.Version {
				addedPermissions, _ := permissionDelta(installedRelease, release)
				if len(addedPermissions) > 0 && !req.AcceptPermissionChanges {
					consentRequired = true
					return nil, nil
				}
			}
		}
		now := time.Now().UTC()
		installedAt := now
		if existing := installs[appID]; existing != nil && !existing.InstalledAt.IsZero() {
			installedAt = existing.InstalledAt
		}
		installs[appID] = &installedAppRef{
			AppID:            appID,
			InstalledVersion: release.Version,
			AutoUpdate:       true,
			Enabled:          true,
			InstalledAt:      installedAt,
			UpdatedAt:        now,
		}
		return []auditEntry{{
			AppID:       appID,
			Version:     release.Version,
			ActorUserID: userID,
			Action:      "install.updated",
			CreatedAt:   now,
			Metadata: map[string]any{
				"auto_update": true,
				"enabled":     true,
			},
		}}, nil
	})
	if err != nil {
		respondRegistryError(w, err)
		return
	}
	app := state.Apps[appID]
	release := latestApprovedRelease(app)
	install := userInstalls(state, userID)[appID]
	payload := appResponse(app, release, install)
	if consentRequired {
		payload["install_note"] = "latest approved update requires consent because permissions expanded"
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *appsServer) uninstallAppHandler(w http.ResponseWriter, r *http.Request, appID string) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := currentUserID(r)
	if userID == "" {
		errorJSON(w, http.StatusUnauthorized, "unauthorized", "missing user header")
		return
	}
	if _, err := s.mutateState(r.Context(), func(state *registryState) ([]auditEntry, error) {
		installs := userInstalls(state, userID)
		if installs[appID] == nil {
			return nil, &apiError{Status: http.StatusNotFound, Code: "not_found", Message: "install not found"}
		}
		delete(installs, appID)
		now := time.Now().UTC()
		return []auditEntry{{
			AppID:       appID,
			ActorUserID: userID,
			Action:      "install.removed",
			CreatedAt:   now,
		}}, nil
	}); err != nil {
		respondRegistryError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *appsServer) listInstalledHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := currentUserID(r)
	if userID == "" {
		errorJSON(w, http.StatusUnauthorized, "unauthorized", "missing user header")
		return
	}
	state, err := s.loadState()
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "load_failed", err.Error())
		return
	}
	installs := userInstalls(state, userID)
	items := make([]map[string]any, 0, len(installs))
	appIDs := make([]string, 0, len(installs))
	for appID := range installs {
		appIDs = append(appIDs, appID)
	}
	slices.Sort(appIDs)
	for _, appID := range appIDs {
		install := installs[appID]
		app := state.Apps[appID]
		if app == nil {
			continue
		}
		release := app.Releases[install.InstalledVersion]
		if release == nil {
			release = latestVisibleRelease(app, userID, isAdmin(r), developerModeEnabled(r))
			if release == nil {
				continue
			}
		}
		if !matchesCatalogFilters(app, release, install, r, isAdmin(r)) {
			continue
		}
		items = append(items, appResponse(app, release, install))
	}
	limit, offset := parseLimitOffset(r, 50)
	total := len(items)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	nextCursor := ""
	if end < total {
		nextCursor = strconv.Itoa(end)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items[offset:end], "next_cursor": nextCursor, "total": total})
}

func (s *appsServer) appUpdatesHandler(w http.ResponseWriter, r *http.Request, appID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := currentUserID(r)
	if userID == "" {
		errorJSON(w, http.StatusUnauthorized, "unauthorized", "missing user header")
		return
	}
	state, err := s.loadState()
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "load_failed", err.Error())
		return
	}
	app := state.Apps[appID]
	if app == nil {
		errorJSON(w, http.StatusNotFound, "not_found", "app not found")
		return
	}
	install := userInstalls(state, userID)[appID]
	latest := latestApprovedRelease(app)
	var updateRequiresConsent bool
	var addedPermissions []string
	var removedPermissions []string
	if install != nil && latest != nil && install.InstalledVersion != "" {
		addedPermissions, removedPermissions = permissionDelta(app.Releases[install.InstalledVersion], latest)
		updateRequiresConsent = len(addedPermissions) > 0
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"app_id": appID,
		"installed_version": func() string {
			if install == nil {
				return ""
			}
			return install.InstalledVersion
		}(),
		"latest_version": func() string {
			if latest == nil {
				return ""
			}
			return latest.Version
		}(),
		"latest_approved_version": func() string {
			if latest == nil {
				return ""
			}
			return latest.Version
		}(),
		"update_available":        latest != nil && install != nil && install.InstalledVersion != latest.Version,
		"update_requires_consent": updateRequiresConsent,
		"permission_delta": map[string]any{
			"added":   addedPermissions,
			"removed": removedPermissions,
		},
	})
}

func (s *appsServer) publisherCreateAppHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := currentUserID(r)
	if userID == "" {
		errorJSON(w, http.StatusUnauthorized, "unauthorized", "missing user header")
		return
	}
	var req struct {
		AppID      string `json:"app_id"`
		Name       string `json:"name"`
		Summary    string `json:"summary"`
		Visibility string `json:"visibility"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid_request", "invalid body")
		return
	}
	if strings.TrimSpace(req.AppID) == "" || strings.TrimSpace(req.Name) == "" {
		errorJSON(w, http.StatusBadRequest, "invalid_request", "app_id and name are required")
		return
	}
	appID := strings.TrimSpace(req.AppID)
	state, err := s.mutateState(r.Context(), func(state *registryState) ([]auditEntry, error) {
		if state.Apps[appID] != nil {
			return nil, &apiError{Status: http.StatusConflict, Code: "conflict", Message: "app already exists"}
		}
		now := time.Now().UTC()
		visibility := "public"
		if strings.ToLower(strings.TrimSpace(req.Visibility)) == "private" {
			visibility = "private"
		}
		state.Apps[appID] = &registeredApp{
			AppID:       appID,
			Name:        strings.TrimSpace(req.Name),
			OwnerUserID: userID,
			Visibility:  visibility,
			Summary:     strings.TrimSpace(req.Summary),
			CreatedAt:   now,
			UpdatedAt:   now,
			Releases:    map[string]*appRelease{},
		}
		return []auditEntry{{
			AppID:       appID,
			ActorUserID: userID,
			Action:      "app.created",
			CreatedAt:   now,
			Metadata: map[string]any{
				"visibility": visibility,
			},
		}}, nil
	})
	if err != nil {
		respondRegistryError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, state.Apps[appID])
}

func (s *appsServer) publisherCreateReleaseHandler(w http.ResponseWriter, r *http.Request, appID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := currentUserID(r)
	if userID == "" {
		errorJSON(w, http.StatusUnauthorized, "unauthorized", "missing user header")
		return
	}
	manifest, err := decodeManifestPayload(r)
	if err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid_request", "invalid manifest payload")
		return
	}
	if stringField(manifest, "app_id") != appID {
		errorJSON(w, http.StatusBadRequest, "invalid_request", "manifest app_id must match route")
		return
	}
	if err := validateManifest(manifest); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid_manifest", err.Error())
		return
	}
	version := stringField(manifest, "version")
	state, err := s.mutateState(r.Context(), func(state *registryState) ([]auditEntry, error) {
		app := state.Apps[appID]
		if app == nil {
			return nil, &apiError{Status: http.StatusNotFound, Code: "not_found", Message: "app not found"}
		}
		if app.OwnerUserID != userID && !isAdmin(r) {
			return nil, &apiError{Status: http.StatusForbidden, Code: "forbidden", Message: "publisher does not own app"}
		}
		if app.Releases[version] != nil {
			return nil, &apiError{Status: http.StatusConflict, Code: "conflict", Message: "release already exists"}
		}
		now := time.Now().UTC()
		payload, _ := json.Marshal(manifest)
		app.Releases[version] = &appRelease{
			Version:            version,
			Manifest:           manifest,
			ManifestHash:       manifestHash(payload),
			ReviewStatus:       statusDraft,
			SourceType:         manifestSourceType(manifest),
			Visibility:         app.Visibility,
			PublisherUserID:    userID,
			SupportedPlatforms: manifestPlatforms(manifest),
			EntrypointOrigin:   manifestOrigin(stringField(manifest["entrypoint"].(map[string]any), "url")),
			PreviewOrigin:      manifestOrigin(stringField(manifest["message_preview"].(map[string]any), "url")),
			CreatedAt:          now,
		}
		app.UpdatedAt = now
		refreshLatestPointers(app)
		return []auditEntry{{
			AppID:       appID,
			Version:     version,
			ActorUserID: userID,
			Action:      "release.created",
			CreatedAt:   now,
			Metadata: map[string]any{
				"source_type": manifestSourceType(manifest),
			},
		}}, nil
	})
	if err != nil {
		respondRegistryError(w, err)
		return
	}
	app := state.Apps[appID]
	writeJSON(w, http.StatusCreated, appResponse(app, app.Releases[version], nil))
}

func (s *appsServer) publisherListReleasesHandler(w http.ResponseWriter, r *http.Request, appID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := currentUserID(r)
	if userID == "" {
		errorJSON(w, http.StatusUnauthorized, "unauthorized", "missing user header")
		return
	}
	state, err := s.loadState()
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "load_failed", err.Error())
		return
	}
	app := state.Apps[appID]
	if app == nil {
		errorJSON(w, http.StatusNotFound, "not_found", "app not found")
		return
	}
	if app.OwnerUserID != userID && !isAdmin(r) {
		errorJSON(w, http.StatusForbidden, "forbidden", "publisher does not own app")
		return
	}
	items := make([]map[string]any, 0, len(app.Releases))
	versions := make([]string, 0, len(app.Releases))
	for version := range app.Releases {
		versions = append(versions, version)
	}
	slices.Sort(versions)
	slices.Reverse(versions)
	for _, version := range versions {
		release := app.Releases[version]
		if query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q"))); query != "" {
			joined := strings.ToLower(strings.Join([]string{app.AppID, app.Name, app.Summary, release.Version, release.ReviewStatus}, " "))
			if !strings.Contains(joined, query) {
				continue
			}
		}
		if status := strings.TrimSpace(r.URL.Query().Get("review_status")); status != "" && release.ReviewStatus != status {
			continue
		}
		items = append(items, appResponse(app, release, nil))
	}
	limit, offset := parseLimitOffset(r, 50)
	total := len(items)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	nextCursor := ""
	if end < total {
		nextCursor = strconv.Itoa(end)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items[offset:end], "next_cursor": nextCursor, "total": total})
}

func (s *appsServer) transitionRelease(w http.ResponseWriter, r *http.Request, appID, version, nextStatus, reviewNote string, adminOnly bool) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := currentUserID(r)
	if userID == "" {
		errorJSON(w, http.StatusUnauthorized, "unauthorized", "missing user header")
		return
	}
	state, err := s.mutateState(r.Context(), func(state *registryState) ([]auditEntry, error) {
		app := state.Apps[appID]
		if app == nil {
			return nil, &apiError{Status: http.StatusNotFound, Code: "not_found", Message: "app not found"}
		}
		if !adminOnly && app.OwnerUserID != userID && !isAdmin(r) {
			return nil, &apiError{Status: http.StatusForbidden, Code: "forbidden", Message: "publisher does not own app"}
		}
		release := app.Releases[version]
		if release == nil {
			return nil, &apiError{Status: http.StatusNotFound, Code: "not_found", Message: "release not found"}
		}
		now := time.Now().UTC()
		switch nextStatus {
		case statusSubmitted:
			release.ReviewStatus = statusSubmitted
			release.SubmittedAt = &now
		case statusUnderReview:
			release.ReviewStatus = statusUnderReview
			release.ReviewedAt = &now
			release.ReviewNote = strings.TrimSpace(reviewNote)
		case statusNeedsChanges:
			release.ReviewStatus = statusNeedsChanges
			release.ReviewedAt = &now
			release.ReviewNote = strings.TrimSpace(reviewNote)
		case statusApproved:
			// Validate manifest immutability before approval
			if err := validateManifestImmutability(release); err != nil {
				return nil, &apiError{
					Status:  http.StatusBadRequest,
					Code:    "immutability_violation",
					Message: err.Error(),
				}
			}

			release.ReviewStatus = statusApproved
			release.ReviewedAt = &now
			release.PublishedAt = &now
			release.RevokedAt = nil
			release.ImmutableAt = &now

			// Compute asset set hash for integrity binding
			assetHashes := []string{
				release.ManifestContentHash, // Manifest hash is base asset
			}
			if release.EntrypointOrigin != "" {
				assetHashes = append(assetHashes, release.EntrypointOrigin)
			}
			if release.PreviewOrigin != "" {
				assetHashes = append(assetHashes, release.PreviewOrigin)
			}
			slices.Sort(assetHashes)
			release.AssetSetHash = computeAssetSetHash(assetHashes)

			// Detect permission expansion compared to previous approved version
			previousApproved := latestApprovedRelease(app)
			if previousApproved != nil && previousApproved.Version != version {
				previousPerms := extractPermissions(previousApproved.Manifest)
				currentPerms := extractPermissions(release.Manifest)
				if isPermissionExpansion(previousPerms, currentPerms) {
					release.RequiresReconsent = true
					release.PreviousPermissions = previousPerms
				}
			}
		case statusRejected:
			release.ReviewStatus = statusRejected
			release.ReviewedAt = &now
			release.ReviewNote = strings.TrimSpace(reviewNote)
		case statusRevoked:
			release.ReviewStatus = statusRevoked
			release.RevokedAt = &now
		case statusSuspended:
			release.ReviewStatus = statusSuspended
			release.ReviewedAt = &now
			release.ReviewNote = strings.TrimSpace(reviewNote)
		default:
			return nil, &apiError{Status: http.StatusBadRequest, Code: "invalid_transition", Message: "unsupported review transition"}
		}
		app.UpdatedAt = now
		refreshLatestPointers(app)
		return []auditEntry{{
			AppID:       appID,
			Version:     version,
			ActorUserID: userID,
			Action:      "release.transition",
			Note:        strings.TrimSpace(reviewNote),
			CreatedAt:   now,
			Metadata: map[string]any{
				"next_status": nextStatus,
			},
		}}, nil
	})
	if err != nil {
		respondRegistryError(w, err)
		return
	}
	app := state.Apps[appID]
	writeJSON(w, http.StatusOK, appResponse(app, app.Releases[version], userInstalls(state, userID)[appID]))
}

func (s *appsServer) publisherSubmitReleaseHandler(w http.ResponseWriter, r *http.Request, appID, version string) {
	s.transitionRelease(w, r, appID, version, statusSubmitted, "", false)
}

func (s *appsServer) publisherRevokeReleaseHandler(w http.ResponseWriter, r *http.Request, appID, version string) {
	s.transitionRelease(w, r, appID, version, statusRevoked, "", false)
}

func (s *appsServer) adminApproveReleaseHandler(w http.ResponseWriter, r *http.Request, appID, version string) {
	if !isAdmin(r) {
		errorJSON(w, http.StatusForbidden, "forbidden", "admin role required")
		return
	}

	// For production (non-dev) releases, verify signature against registered publisher key
	state, err := s.loadState()
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "load_failed", err.Error())
		return
	}

	app := state.Apps[appID]
	if app == nil {
		errorJSON(w, http.StatusNotFound, "not_found", "app not found")
		return
	}

	release := app.Releases[version]
	if release == nil {
		errorJSON(w, http.StatusNotFound, "not_found", "release not found")
		return
	}

	// Check signature for production releases (but not dev releases)
	sourceType := release.SourceType
	if sourceType != "dev" && s.pool != nil {
		manifest := release.Manifest
		if manifest != nil {
			_, _, err := s.verifyReleaseSignature(r.Context(), manifest, release.PublisherUserID)
			if err != nil {
				errorJSON(w, http.StatusBadRequest, "signature_invalid", fmt.Sprintf("production release requires valid signature: %v", err))
				return
			}
		}
	}

	s.transitionRelease(w, r, appID, version, statusApproved, "", true)
}

func (s *appsServer) adminRejectReleaseHandler(w http.ResponseWriter, r *http.Request, appID, version string) {
	if !isAdmin(r) {
		errorJSON(w, http.StatusForbidden, "forbidden", "admin role required")
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	s.transitionRelease(w, r, appID, version, statusRejected, req.Reason, true)
}

func (s *appsServer) adminStartReviewHandler(w http.ResponseWriter, r *http.Request, appID, version string) {
	if !isAdmin(r) {
		errorJSON(w, http.StatusForbidden, "forbidden", "admin role required")
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	s.transitionRelease(w, r, appID, version, statusUnderReview, req.Reason, true)
}

func (s *appsServer) adminNeedsChangesReleaseHandler(w http.ResponseWriter, r *http.Request, appID, version string) {
	if !isAdmin(r) {
		errorJSON(w, http.StatusForbidden, "forbidden", "admin role required")
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	s.transitionRelease(w, r, appID, version, statusNeedsChanges, req.Reason, true)
}

func (s *appsServer) adminSuspendReleaseHandler(w http.ResponseWriter, r *http.Request, appID, version string) {
	if !isAdmin(r) {
		errorJSON(w, http.StatusForbidden, "forbidden", "admin role required")
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	s.transitionRelease(w, r, appID, version, statusSuspended, req.Reason, true)
}

func (s *appsServer) route(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimSpace(r.URL.Path), "/")
	switch {
	case path == "v1/apps/register":
		s.registerHandler(w, r)
	case path == "v1/apps":
		s.listAppsHandler(w, r)
	case path == "v1/apps/installed":
		s.listInstalledHandler(w, r)
	case strings.HasPrefix(path, "v1/apps/") && strings.HasSuffix(path, "/install"):
		appID := strings.TrimSuffix(strings.TrimPrefix(path, "v1/apps/"), "/install")
		if r.Method == http.MethodPost {
			s.installAppHandler(w, r, appID)
			return
		}
		if r.Method == http.MethodDelete {
			s.uninstallAppHandler(w, r, appID)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	case strings.HasPrefix(path, "v1/apps/") && strings.HasSuffix(path, "/updates"):
		appID := strings.TrimSuffix(strings.TrimPrefix(path, "v1/apps/"), "/updates")
		s.appUpdatesHandler(w, r, appID)
	case strings.HasPrefix(path, "v1/apps/"):
		appID := strings.TrimPrefix(path, "v1/apps/")
		s.getAppHandler(w, r, appID)
	case path == "v1/publisher/apps":
		s.publisherCreateAppHandler(w, r)
	case path == "v1/publisher/keys":
		if r.Method == http.MethodPost {
			s.publisherRegisterKeyHandler(w, r)
			return
		}
		if r.Method == http.MethodGet {
			s.publisherListKeysHandler(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	case strings.HasPrefix(path, "v1/publisher/keys/") && strings.HasSuffix(path, "/rotate"):
		keyID := strings.TrimSuffix(strings.TrimPrefix(path, "v1/publisher/keys/"), "/rotate")
		if !strings.Contains(keyID, "/") {
			s.publisherRotateKeyHandler(w, r, keyID)
			return
		}
		http.NotFound(w, r)
	case strings.HasPrefix(path, "v1/publisher/keys/"):
		keyID := strings.TrimPrefix(path, "v1/publisher/keys/")
		if !strings.Contains(keyID, "/") {
			s.publisherRevokeKeyHandler(w, r, keyID)
			return
		}
		http.NotFound(w, r)
	case strings.HasPrefix(path, "v1/publisher/apps/") && strings.HasSuffix(path, "/releases"):
		rest := strings.TrimSuffix(strings.TrimPrefix(path, "v1/publisher/apps/"), "/releases")
		if strings.Contains(rest, "/") {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodPost {
			s.publisherCreateReleaseHandler(w, r, rest)
			return
		}
		if r.Method == http.MethodGet {
			s.publisherListReleasesHandler(w, r, rest)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	case strings.HasPrefix(path, "v1/publisher/apps/") && strings.Contains(path, "/releases/") && strings.HasSuffix(path, "/submit"):
		parts := strings.Split(strings.TrimPrefix(path, "v1/publisher/apps/"), "/releases/")
		s.publisherSubmitReleaseHandler(w, r, parts[0], strings.TrimSuffix(parts[1], "/submit"))
	case strings.HasPrefix(path, "v1/publisher/apps/") && strings.Contains(path, "/releases/") && strings.HasSuffix(path, "/revoke"):
		parts := strings.Split(strings.TrimPrefix(path, "v1/publisher/apps/"), "/releases/")
		s.publisherRevokeReleaseHandler(w, r, parts[0], strings.TrimSuffix(parts[1], "/revoke"))
	case strings.HasPrefix(path, "v1/admin/apps/") && strings.Contains(path, "/releases/") && strings.HasSuffix(path, "/approve"):
		parts := strings.Split(strings.TrimPrefix(path, "v1/admin/apps/"), "/releases/")
		s.adminApproveReleaseHandler(w, r, parts[0], strings.TrimSuffix(parts[1], "/approve"))
	case strings.HasPrefix(path, "v1/admin/apps/") && strings.Contains(path, "/releases/") && strings.HasSuffix(path, "/start-review"):
		parts := strings.Split(strings.TrimPrefix(path, "v1/admin/apps/"), "/releases/")
		s.adminStartReviewHandler(w, r, parts[0], strings.TrimSuffix(parts[1], "/start-review"))
	case strings.HasPrefix(path, "v1/admin/apps/") && strings.Contains(path, "/releases/") && strings.HasSuffix(path, "/needs-changes"):
		parts := strings.Split(strings.TrimPrefix(path, "v1/admin/apps/"), "/releases/")
		s.adminNeedsChangesReleaseHandler(w, r, parts[0], strings.TrimSuffix(parts[1], "/needs-changes"))
	case strings.HasPrefix(path, "v1/admin/apps/") && strings.Contains(path, "/releases/") && strings.HasSuffix(path, "/reject"):
		parts := strings.Split(strings.TrimPrefix(path, "v1/admin/apps/"), "/releases/")
		s.adminRejectReleaseHandler(w, r, parts[0], strings.TrimSuffix(parts[1], "/reject"))
	case strings.HasPrefix(path, "v1/admin/apps/") && strings.Contains(path, "/releases/") && strings.HasSuffix(path, "/suspend"):
		parts := strings.Split(strings.TrimPrefix(path, "v1/admin/apps/"), "/releases/")
		s.adminSuspendReleaseHandler(w, r, parts[0], strings.TrimSuffix(parts[1], "/suspend"))
	default:
		http.NotFound(w, r)
	}
}

// latestApprovedRelease finds the most recent approved release for an app
func latestApprovedRelease(app *registeredApp) *appRelease {
	var latest *appRelease
	for _, rel := range app.Releases {
		if rel.ReviewStatus != statusApproved {
			continue
		}
		if latest == nil || (rel.PublishedAt != nil && latest.PublishedAt != nil && rel.PublishedAt.After(*latest.PublishedAt)) {
			latest = rel
		}
	}
	return latest
}

// extractPermissions extracts the permissions array from a manifest
func extractPermissions(manifest map[string]any) []string {
	if manifest == nil {
		return []string{}
	}
	perms, ok := manifest["permissions"].([]any)
	if !ok || len(perms) == 0 {
		return []string{}
	}
	result := make([]string, 0, len(perms))
	for _, p := range perms {
		if s, ok := p.(string); ok && s != "" {
			result = append(result, s)
		}
	}
	return result
}

// isPermissionExpansion checks if currentPerms is a superset of previousPerms (i.e., expansion detected)
func isPermissionExpansion(previousPerms, currentPerms []string) bool {
	if len(currentPerms) <= len(previousPerms) {
		return false
	}
	// Create a set of previous permissions
	prevSet := make(map[string]struct{}, len(previousPerms))
	for _, p := range previousPerms {
		prevSet[p] = struct{}{}
	}
	// Check if all current permissions are in previous (if so, no expansion)
	for _, p := range currentPerms {
		if _, exists := prevSet[p]; !exists {
			return true // Found a new permission
		}
	}
	return false
}

// validatePublicKeyPEM validates the PEM-encoded public key format matches the algorithm.
func validatePublicKeyPEM(pubKeyPEM string, algorithm string) bool {
	block, _ := pem.Decode([]byte(pubKeyPEM))
	if block == nil {
		return false
	}
	pk, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return false
	}
	switch algorithm {
	case "RS256":
		_, ok := pk.(*rsa.PublicKey)
		return ok
	case "Ed25519", "EdDSA":
		_, ok := pk.(ed25519.PublicKey)
		return ok
	default:
		return false
	}
}

// computeKeyFingerprint computes a SHA-256 fingerprint of the PEM-encoded public key.
func computeKeyFingerprint(pubKeyPEM string) string {
	hash := sha256.Sum256([]byte(pubKeyPEM))
	return fmt.Sprintf("%x", hash[:])
}

// verifyReleaseSignature validates that a release manifest is signed with a registered publisher key.
func (s *appsServer) verifyReleaseSignature(ctx context.Context, manifest map[string]any, publisherUserID string) (string, string, error) {
	if s.pool == nil {
		return "", "", nil
	}
	rawSignature, ok := manifest["signature"]
	if !ok || rawSignature == nil {
		return "", "", nil
	}
	sigMap, ok := rawSignature.(map[string]any)
	if !ok {
		return "", "", fmt.Errorf("signature object required")
	}
	alg := extractStringField(sigMap, "alg")
	kid := extractStringField(sigMap, "kid")
	sig := extractStringField(sigMap, "sig")
	if alg == "" || kid == "" || sig == "" {
		return "", "", fmt.Errorf("signature.alg, signature.kid, and signature.sig are required")
	}
	var pubKeyPEM string
	var isActive bool
	if err := s.pool.QueryRow(ctx, `
		SELECT public_key, is_active FROM miniapp_registry_publisher_keys
		WHERE publisher_user_id = $1 AND key_id = $2
	`, publisherUserID, kid).Scan(&pubKeyPEM, &isActive); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", fmt.Errorf("signer key not found")
		}
		return "", "", err
	}
	if !isActive {
		return "", "", fmt.Errorf("signer key is revoked")
	}
	if err := verifyManifestSignatureWithKey(manifest, pubKeyPEM, alg, sig); err != nil {
		return "", "", err
	}
	return kid, alg, nil
}

// verifyManifestSignatureWithKey verifies a manifest signature using the provided public key.
func verifyManifestSignatureWithKey(manifest map[string]any, pubKeyPEM string, alg string, sigBase64 string) error {
	copyMap := make(map[string]any, len(manifest))
	for k, v := range manifest {
		if k == "signature" {
			continue
		}
		copyMap[k] = v
	}
	payload, err := json.Marshal(copyMap)
	if err != nil {
		return err
	}
	sigBytes, err := base64.StdEncoding.DecodeString(sigBase64)
	if err != nil {
		return fmt.Errorf("invalid signature encoding: %v", err)
	}
	block, _ := pem.Decode([]byte(pubKeyPEM))
	if block == nil {
		return fmt.Errorf("invalid public key PEM")
	}
	pk, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return err
	}
	switch alg {
	case "RS256":
		rsaKey, ok := pk.(*rsa.PublicKey)
		if !ok {
			return fmt.Errorf("public key does not support RS256")
		}
		h := sha256.Sum256(payload)
		return rsa.VerifyPKCS1v15(rsaKey, crypto.SHA256, h[:], sigBytes)
	case "Ed25519", "EdDSA":
		edKey, ok := pk.(ed25519.PublicKey)
		if !ok {
			return fmt.Errorf("public key does not support Ed25519")
		}
		if !ed25519.Verify(edKey, payload, sigBytes) {
			return fmt.Errorf("signature verification failed")
		}
		return nil
	default:
		return fmt.Errorf("unsupported signature algorithm %q", alg)
	}
}

// decodeBase64String decodes a base64-encoded string.
func decodeBase64String(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

// extractStringField safely extracts a string field from a map.
func extractStringField(m map[string]any, key string) string {
	value, ok := m[key].(string)
	if !ok {
		return ""
	}
	return value
}

// publisherRegisterKeyHandler: POST /v1/publisher/keys
// Registers a new RSA or Ed25519 public key for manifest signing.
func (s *appsServer) publisherRegisterKeyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := currentUserID(r)
	if userID == "" {
		errorJSON(w, http.StatusUnauthorized, "unauthorized", "missing user header")
		return
	}
	var req struct {
		KeyID     string `json:"key_id"`
		Algorithm string `json:"algorithm"`
		PublicKey string `json:"public_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid_request", "invalid body")
		return
	}
	if strings.TrimSpace(req.KeyID) == "" || strings.TrimSpace(req.Algorithm) == "" || strings.TrimSpace(req.PublicKey) == "" {
		errorJSON(w, http.StatusBadRequest, "invalid_request", "key_id, algorithm, and public_key are required")
		return
	}
	algorithm := strings.TrimSpace(req.Algorithm)
	if algorithm != "RS256" && algorithm != "Ed25519" && algorithm != "EdDSA" {
		errorJSON(w, http.StatusBadRequest, "invalid_request", "algorithm must be RS256, Ed25519, or EdDSA")
		return
	}
	keyID := strings.TrimSpace(req.KeyID)
	pubKeyPEM := strings.TrimSpace(req.PublicKey)
	if !validatePublicKeyPEM(pubKeyPEM, algorithm) {
		errorJSON(w, http.StatusBadRequest, "invalid_request", "invalid public key format for algorithm")
		return
	}
	keyFingerprint := computeKeyFingerprint(pubKeyPEM)
	if s.pool != nil {
		if _, err := s.pool.Exec(r.Context(), `
			INSERT INTO miniapp_registry_publisher_keys
			(publisher_user_id, key_id, algorithm, public_key, key_fingerprint, is_active, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, now(), now())
			ON CONFLICT (publisher_user_id, key_id) DO NOTHING
		`, userID, keyID, algorithm, pubKeyPEM, keyFingerprint, true); err != nil {
			if strings.Contains(err.Error(), "duplicate") {
				errorJSON(w, http.StatusConflict, "conflict", "key already registered")
			} else {
				errorJSON(w, http.StatusInternalServerError, "db_error", err.Error())
			}
			return
		}
		if _, err := s.pool.Exec(r.Context(), `
			INSERT INTO miniapp_publisher_key_operations
			(publisher_user_id, key_id, operation, actor_user_id, created_at)
			VALUES ($1, $2, $3, $4, now())
		`, userID, keyID, "register", userID); err != nil {
			observability.Logger.Printf("failed to log key registration: %v", err)
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"key_id":      keyID,
		"algorithm":   algorithm,
		"fingerprint": keyFingerprint,
		"created_at":  time.Now().UTC(),
	})
}

// publisherListKeysHandler: GET /v1/publisher/keys
// Lists all public keys registered by the current publisher.
func (s *appsServer) publisherListKeysHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := currentUserID(r)
	if userID == "" {
		errorJSON(w, http.StatusUnauthorized, "unauthorized", "missing user header")
		return
	}
	if s.pool == nil {
		errorJSON(w, http.StatusNotImplemented, "not_available", "publisher keys require database persistence")
		return
	}
	rows, err := s.pool.Query(r.Context(), `
		SELECT key_id, algorithm, key_fingerprint, created_at, updated_at, is_active, revoked_at
		FROM miniapp_registry_publisher_keys
		WHERE publisher_user_id = $1
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	defer rows.Close()
	keys := make([]map[string]any, 0)
	for rows.Next() {
		var keyID, algorithm, fingerprint string
		var createdAt, updatedAt time.Time
		var isActive bool
		var revokedAt *time.Time
		if err := rows.Scan(&keyID, &algorithm, &fingerprint, &createdAt, &updatedAt, &isActive, &revokedAt); err != nil {
			errorJSON(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		status := "active"
		if revokedAt != nil {
			status = "revoked"
		}
		keys = append(keys, map[string]any{
			"key_id":      keyID,
			"algorithm":   algorithm,
			"fingerprint": fingerprint,
			"status":      status,
			"created_at":  createdAt,
			"updated_at":  updatedAt,
			"revoked_at":  revokedAt,
		})
	}
	if err := rows.Err(); err != nil {
		errorJSON(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": keys})
}

// publisherRevokeKeyHandler: DELETE /v1/publisher/keys/{kid}
// Revokes a publisher's cryptographic key, preventing future release signatures with it.
func (s *appsServer) publisherRevokeKeyHandler(w http.ResponseWriter, r *http.Request, keyID string) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := currentUserID(r)
	if userID == "" {
		errorJSON(w, http.StatusUnauthorized, "unauthorized", "missing user header")
		return
	}
	if s.pool == nil {
		errorJSON(w, http.StatusNotImplemented, "not_available", "key revocation requires database persistence")
		return
	}
	var reason string
	if r.Body != nil {
		var req struct {
			Reason string `json:"reason"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		reason = strings.TrimSpace(req.Reason)
	}
	result, err := s.pool.Exec(r.Context(), `
		UPDATE miniapp_registry_publisher_keys
		SET revoked_at = now(), is_active = false, updated_at = now()
		WHERE publisher_user_id = $1 AND key_id = $2 AND revoked_at IS NULL
	`, userID, keyID)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	if result.RowsAffected() == 0 {
		errorJSON(w, http.StatusNotFound, "not_found", "key not found or already revoked")
		return
	}
	if _, err := s.pool.Exec(r.Context(), `
		INSERT INTO miniapp_publisher_key_operations
		(publisher_user_id, key_id, operation, operation_reason, actor_user_id, created_at)
		VALUES ($1, $2, $3, $4, $5, now())
	`, userID, keyID, "revoke", reason, userID); err != nil {
		observability.Logger.Printf("failed to log key revocation: %v", err)
	}
	w.WriteHeader(http.StatusNoContent)
}

// publisherRotateKeyHandler: POST /v1/publisher/keys/{old_kid}/rotate
// Rotates to a new key while maintaining a grace period for the old key (graceful transition).
func (s *appsServer) publisherRotateKeyHandler(w http.ResponseWriter, r *http.Request, oldKeyID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := currentUserID(r)
	if userID == "" {
		errorJSON(w, http.StatusUnauthorized, "unauthorized", "missing user header")
		return
	}
	if s.pool == nil {
		errorJSON(w, http.StatusNotImplemented, "not_available", "key rotation requires database persistence")
		return
	}
	var req struct {
		NewKeyID string `json:"new_key_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid_request", "invalid body")
		return
	}
	if strings.TrimSpace(req.NewKeyID) == "" {
		errorJSON(w, http.StatusBadRequest, "invalid_request", "new_key_id is required")
		return
	}
	newKeyID := strings.TrimSpace(req.NewKeyID)
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	var oldKeyExists bool
	if err := tx.QueryRow(r.Context(), `
		SELECT true FROM miniapp_registry_publisher_keys
		WHERE publisher_user_id = $1 AND key_id = $2 AND is_active = true
	`, userID, oldKeyID).Scan(&oldKeyExists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			errorJSON(w, http.StatusNotFound, "not_found", "old key not found or inactive")
		} else {
			errorJSON(w, http.StatusInternalServerError, "db_error", err.Error())
		}
		return
	}
	var newKeyExists bool
	if err := tx.QueryRow(r.Context(), `
		SELECT true FROM miniapp_registry_publisher_keys
		WHERE publisher_user_id = $1 AND key_id = $2
	`, userID, newKeyID).Scan(&newKeyExists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			errorJSON(w, http.StatusBadRequest, "invalid_request", "new key not found for this publisher")
		} else {
			errorJSON(w, http.StatusInternalServerError, "db_error", err.Error())
		}
		return
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE miniapp_registry_publisher_keys
		SET rotated_to_key_id = $1, updated_at = now()
		WHERE publisher_user_id = $2 AND key_id = $3
	`, newKeyID, userID, oldKeyID); err != nil {
		errorJSON(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	if _, err := tx.Exec(r.Context(), `
		INSERT INTO miniapp_publisher_key_operations
		(publisher_user_id, key_id, operation, prev_key_id, actor_user_id, created_at)
		VALUES ($1, $2, $3, $4, $5, now())
	`, userID, newKeyID, "rotate_to", oldKeyID, userID); err != nil {
		observability.Logger.Printf("failed to log key rotation: %v", err)
	}
	if err := tx.Commit(r.Context()); err != nil {
		errorJSON(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"old_key_id": oldKeyID,
		"new_key_id": newKeyID,
		"rotated_at": time.Now().UTC(),
	})
}

func makeHandler(s *appsServer) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/v1/apps/", http.HandlerFunc(s.route))
	mux.Handle("/v1/apps", http.HandlerFunc(s.route))
	mux.Handle("/v1/publisher/apps/", http.HandlerFunc(s.route))
	mux.Handle("/v1/publisher/apps", http.HandlerFunc(s.route))
	mux.Handle("/v1/publisher/keys/", http.HandlerFunc(s.route))
	mux.Handle("/v1/publisher/keys", http.HandlerFunc(s.route))
	mux.Handle("/v1/admin/apps/", http.HandlerFunc(s.route))
	mux.Handle("/metrics", observability.MetricsHandler())
	mux.Handle("/healthz", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	return observability.RequestIDMiddleware(mux)
}
