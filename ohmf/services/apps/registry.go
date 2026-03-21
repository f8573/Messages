package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var semverPattern = regexp.MustCompile(`^\d+\.\d+\.\d+(-[A-Za-z0-9.-]+)?$`)

const (
	statusDraft        = "draft"
	statusSubmitted    = "submitted"
	statusUnderReview  = "under_review"
	statusNeedsChanges = "needs_changes"
	statusApproved     = "approved"
	statusRejected     = "rejected"
	statusSuspended    = "suspended"
	statusRevoked      = "revoked"

	registryAdvisoryLockID int64 = 88442211337755
)

type registryState struct {
	Apps     map[string]*registeredApp              `json:"apps"`
	Installs map[string]map[string]*installedAppRef `json:"installs"`
}

type registeredApp struct {
	AppID          string                 `json:"app_id"`
	Name           string                 `json:"name"`
	OwnerUserID    string                 `json:"owner_user_id"`
	Visibility     string                 `json:"visibility"`
	Summary        string                 `json:"summary,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
	LatestVersion  string                 `json:"latest_version,omitempty"`
	LatestApproved string                 `json:"latest_approved_version,omitempty"`
	Releases       map[string]*appRelease `json:"releases"`
}

type appRelease struct {
	Version            string         `json:"version"`
	Manifest           map[string]any `json:"manifest"`
	ManifestHash       string         `json:"manifest_hash"`
	ReviewStatus       string         `json:"review_status"`
	ReviewNote         string         `json:"review_note,omitempty"`
	SourceType         string         `json:"source_type"`
	Visibility         string         `json:"visibility"`
	PublisherUserID    string         `json:"publisher_user_id"`
	SupportedPlatforms []string       `json:"supported_platforms,omitempty"`
	EntrypointOrigin   string         `json:"entrypoint_origin,omitempty"`
	PreviewOrigin      string         `json:"preview_origin,omitempty"`
	CreatedAt          time.Time      `json:"created_at"`
	SubmittedAt        *time.Time     `json:"submitted_at,omitempty"`
	ReviewedAt         *time.Time     `json:"reviewed_at,omitempty"`
	PublishedAt        *time.Time     `json:"published_at,omitempty"`
	RevokedAt          *time.Time     `json:"revoked_at,omitempty"`
	SuspendedAt        *time.Time     `json:"suspended_at,omitempty"`
	SuspensionReason   string         `json:"suspension_reason,omitempty"`
	// Immutable release packaging
	ManifestContentHash string         `json:"manifest_content_hash,omitempty"`
	AssetSetHash        string         `json:"asset_set_hash,omitempty"`
	ImmutableAt         *time.Time     `json:"immutable_at,omitempty"`
	// Permission expansion tracking
	RequiresReconsent    bool           `json:"requires_reconsent"`
	PreviousPermissions []string       `json:"previous_permissions,omitempty"`
}

type installedAppRef struct {
	AppID            string    `json:"app_id"`
	InstalledVersion string    `json:"installed_version"`
	AutoUpdate       bool      `json:"auto_update"`
	Enabled          bool      `json:"enabled"`
	InstalledAt      time.Time `json:"installed_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type auditEntry struct {
	AppID       string
	Version     string
	ActorUserID string
	Action      string
	Note        string
	Metadata    map[string]any
	CreatedAt   time.Time
}

type apiError struct {
	Status  int
	Code    string
	Message string
}

func (e *apiError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

type appsServer struct {
	mu       sync.Mutex
	dataFile string
	pool     *pgxpool.Pool
}

type registryQueryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type registryExecer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

type registryTx interface {
	registryQueryer
	registryExecer
}

func newAppsServer(dataFile string) *appsServer {
	return &appsServer{dataFile: dataFile}
}

func newAppsServerWithDB(dataFile string, pool *pgxpool.Pool) *appsServer {
	return &appsServer{dataFile: dataFile, pool: pool}
}

func respondRegistryError(w http.ResponseWriter, err error) {
	var apiErr *apiError
	if errors.As(err, &apiErr) {
		errorJSON(w, apiErr.Status, apiErr.Code, apiErr.Message)
		return
	}
	errorJSON(w, http.StatusInternalServerError, "internal_error", err.Error())
}

func emptyRegistryState() *registryState {
	return &registryState{
		Apps:     map[string]*registeredApp{},
		Installs: map[string]map[string]*installedAppRef{},
	}
}

// computeManifestContentHash returns a SHA-256 hash of the canonical manifest JSON
func computeManifestContentHash(manifestBytes []byte) string {
	h := sha256.Sum256(manifestBytes)
	return fmt.Sprintf("sha256:%x", h)
}

// computeAssetSetHash returns a SHA-256 hash of the concatenated asset hashes
func computeAssetSetHash(assetHashes []string) string {
	h := sha256.New()
	for _, hash := range assetHashes {
		h.Write([]byte(hash))
	}
	return fmt.Sprintf("sha256:%x", h.Sum(nil))
}

// validateManifestImmutability ensures manifest hasn't changed since creation
func validateManifestImmutability(release *appRelease) error {
	if release.ManifestContentHash == "" {
		// Legacy releases without hash field; skip validation
		return nil
	}

	// Recompute hash from stored manifest JSON
	manifestBytes, err := json.Marshal(release.Manifest)
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}
	currentHash := computeManifestContentHash(manifestBytes)

	// Verify hash matches what was stored at creation
	if currentHash != release.ManifestContentHash {
		return fmt.Errorf(
			"manifest_has_changed_after_creation: stored=%s current=%s",
			release.ManifestContentHash,
			currentHash,
		)
	}
	return nil
}

func (s *appsServer) loadState() (*registryState, error) {
	if s.pool != nil {
		return s.loadStateFromDB(context.Background())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadStateFileLocked()
}

func (s *appsServer) saveState(state *registryState) error {
	if s.pool != nil {
		ctx := context.Background()
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, registryAdvisoryLockID); err != nil {
			return err
		}
		if err := s.saveStateToTx(ctx, tx, state); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveStateFileLocked(state)
}

func (s *appsServer) mutateState(ctx context.Context, mutate func(state *registryState) ([]auditEntry, error)) (*registryState, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if s.pool == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		state, err := s.loadStateFileLocked()
		if err != nil {
			return nil, err
		}
		if state == nil {
			state = emptyRegistryState()
		}
		if state.Apps == nil {
			state.Apps = map[string]*registeredApp{}
		}
		if state.Installs == nil {
			state.Installs = map[string]map[string]*installedAppRef{}
		}
		if _, err := mutate(state); err != nil {
			return nil, err
		}
		if err := s.saveStateFileLocked(state); err != nil {
			return nil, err
		}
		return state, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, registryAdvisoryLockID); err != nil {
		return nil, err
	}
	state, err := loadStateFromQuerier(ctx, tx)
	if err != nil {
		return nil, err
	}
	auditEntries, err := mutate(state)
	if err != nil {
		return nil, err
	}
	if err := s.saveStateToTx(ctx, tx, state); err != nil {
		return nil, err
	}
	if err := appendAuditEntriesTx(ctx, tx, auditEntries); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return state, nil
}

func (s *appsServer) loadStateFileLocked() (*registryState, error) {
	state := emptyRegistryState()
	f, err := os.Open(s.dataFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return state, nil
		}
		return nil, err
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(state); err != nil {
		return nil, err
	}
	if state.Apps == nil {
		state.Apps = map[string]*registeredApp{}
	}
	if state.Installs == nil {
		state.Installs = map[string]map[string]*installedAppRef{}
	}
	for _, app := range state.Apps {
		if app.Releases == nil {
			app.Releases = map[string]*appRelease{}
		}
		refreshLatestPointers(app)
	}
	return state, nil
}

func (s *appsServer) saveStateFileLocked(state *registryState) error {
	dir := filepath.Dir(s.dataFile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tempFile, err := os.CreateTemp(dir, "registry-*.json")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	success := false
	defer func() {
		_ = tempFile.Close()
		if !success {
			_ = os.Remove(tempPath)
		}
	}()
	enc := json.NewEncoder(tempFile)
	enc.SetIndent("", "  ")
	if err := enc.Encode(state); err != nil {
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, s.dataFile); err != nil {
		return err
	}
	success = true
	return nil
}

func (s *appsServer) loadStateFromDB(ctx context.Context) (*registryState, error) {
	return loadStateFromQuerier(ctx, s.pool)
}

func loadStateFromQuerier(ctx context.Context, q registryQueryer) (*registryState, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	state := emptyRegistryState()

	appRows, err := q.Query(ctx, `
		SELECT app_id, name, owner_user_id, visibility, summary, created_at, updated_at, latest_version, latest_approved_version
		FROM miniapp_registry_apps
		ORDER BY app_id
	`)
	if err != nil {
		return nil, err
	}
	defer appRows.Close()
	for appRows.Next() {
		app := &registeredApp{Releases: map[string]*appRelease{}}
		if err := appRows.Scan(
			&app.AppID,
			&app.Name,
			&app.OwnerUserID,
			&app.Visibility,
			&app.Summary,
			&app.CreatedAt,
			&app.UpdatedAt,
			&app.LatestVersion,
			&app.LatestApproved,
		); err != nil {
			return nil, err
		}
		state.Apps[app.AppID] = app
	}
	if err := appRows.Err(); err != nil {
		return nil, err
	}

	releaseRows, err := q.Query(ctx, `
		SELECT
			app_id,
			version,
			manifest_json,
			manifest_hash,
			review_status,
			review_note,
			source_type,
			visibility,
			publisher_user_id,
			supported_platforms,
			entrypoint_origin,
			preview_origin,
			created_at,
			submitted_at,
			reviewed_at,
			published_at,
			revoked_at,
			suspended_at,
			suspension_reason,
			manifest_content_hash,
			asset_set_hash,
			immutable_at
		FROM miniapp_registry_releases
		ORDER BY app_id, version
	`)
	if err != nil {
		return nil, err
	}
	defer releaseRows.Close()
	for releaseRows.Next() {
		var (
			appID              string
			manifestRaw        []byte
			supportedPlatforms []byte
			release            appRelease
		)
		if err := releaseRows.Scan(
			&appID,
			&release.Version,
			&manifestRaw,
			&release.ManifestHash,
			&release.ReviewStatus,
			&release.ReviewNote,
			&release.SourceType,
			&release.Visibility,
			&release.PublisherUserID,
			&supportedPlatforms,
			&release.EntrypointOrigin,
			&release.PreviewOrigin,
			&release.CreatedAt,
			&release.SubmittedAt,
			&release.ReviewedAt,
			&release.PublishedAt,
			&release.RevokedAt,
			&release.SuspendedAt,
			&release.SuspensionReason,
			&release.ManifestContentHash,
			&release.AssetSetHash,
			&release.ImmutableAt,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(manifestRaw, &release.Manifest); err != nil {
			return nil, err
		}
		if len(supportedPlatforms) > 0 {
			if err := json.Unmarshal(supportedPlatforms, &release.SupportedPlatforms); err != nil {
				return nil, err
			}
		}
		app := state.Apps[appID]
		if app == nil {
			continue
		}
		copyRelease := release
		app.Releases[release.Version] = &copyRelease
	}
	if err := releaseRows.Err(); err != nil {
		return nil, err
	}

	installRows, err := q.Query(ctx, `
		SELECT user_id, app_id, installed_version, auto_update, enabled, installed_at, updated_at
		FROM miniapp_registry_installs
		ORDER BY user_id, app_id
	`)
	if err != nil {
		return nil, err
	}
	defer installRows.Close()
	for installRows.Next() {
		ref := &installedAppRef{}
		var userID string
		if err := installRows.Scan(
			&userID,
			&ref.AppID,
			&ref.InstalledVersion,
			&ref.AutoUpdate,
			&ref.Enabled,
			&ref.InstalledAt,
			&ref.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if state.Installs[userID] == nil {
			state.Installs[userID] = map[string]*installedAppRef{}
		}
		state.Installs[userID][ref.AppID] = ref
	}
	if err := installRows.Err(); err != nil {
		return nil, err
	}

	for _, app := range state.Apps {
		refreshLatestPointers(app)
	}
	return state, nil
}

func (s *appsServer) saveStateToTx(ctx context.Context, tx registryTx, state *registryState) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, err := tx.Exec(ctx, `DELETE FROM miniapp_registry_installs`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM miniapp_registry_releases`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM miniapp_registry_apps`); err != nil {
		return err
	}

	appIDs := make([]string, 0, len(state.Apps))
	for appID := range state.Apps {
		appIDs = append(appIDs, appID)
	}
	slices.Sort(appIDs)
	for _, appID := range appIDs {
		app := state.Apps[appID]
		if _, err := tx.Exec(ctx, `
			INSERT INTO miniapp_registry_apps (
				app_id, name, owner_user_id, visibility, summary, created_at, updated_at, latest_version, latest_approved_version
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		`,
			app.AppID,
			app.Name,
			app.OwnerUserID,
			app.Visibility,
			app.Summary,
			app.CreatedAt,
			app.UpdatedAt,
			app.LatestVersion,
			app.LatestApproved,
		); err != nil {
			return err
		}

		versions := make([]string, 0, len(app.Releases))
		for version := range app.Releases {
			versions = append(versions, version)
		}
		slices.Sort(versions)
		for _, version := range versions {
			release := app.Releases[version]
			manifestJSON, err := json.Marshal(release.Manifest)
			if err != nil {
				return err
			}
			supportedPlatformsJSON, err := json.Marshal(release.SupportedPlatforms)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO miniapp_registry_releases (
					app_id,
					version,
					manifest_json,
					manifest_hash,
					review_status,
					review_note,
					source_type,
					visibility,
					publisher_user_id,
					supported_platforms,
					entrypoint_origin,
					preview_origin,
					created_at,
					submitted_at,
					reviewed_at,
					published_at,
					revoked_at,
					suspended_at,
					suspension_reason,
					manifest_content_hash,
					asset_set_hash,
					immutable_at
				) VALUES ($1,$2,$3::jsonb,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)
			`,
				app.AppID,
				release.Version,
				string(manifestJSON),
				release.ManifestHash,
				release.ReviewStatus,
				release.ReviewNote,
				release.SourceType,
				release.Visibility,
				release.PublisherUserID,
				string(supportedPlatformsJSON),
				release.EntrypointOrigin,
				release.PreviewOrigin,
				release.CreatedAt,
				release.SubmittedAt,
				release.ReviewedAt,
				release.PublishedAt,
				release.RevokedAt,
				release.SuspendedAt,
				release.SuspensionReason,
				release.ManifestContentHash,
				release.AssetSetHash,
				release.ImmutableAt,
			); err != nil {
				return err
			}
		}
	}

	userIDs := make([]string, 0, len(state.Installs))
	for userID := range state.Installs {
		userIDs = append(userIDs, userID)
	}
	slices.Sort(userIDs)
	for _, userID := range userIDs {
		appInstalls := state.Installs[userID]
		appIDs := make([]string, 0, len(appInstalls))
		for appID := range appInstalls {
			appIDs = append(appIDs, appID)
		}
		slices.Sort(appIDs)
		for _, appID := range appIDs {
			install := appInstalls[appID]
			if _, err := tx.Exec(ctx, `
				INSERT INTO miniapp_registry_installs (
					user_id, app_id, installed_version, auto_update, enabled, installed_at, updated_at
				) VALUES ($1,$2,$3,$4,$5,$6,$7)
			`,
				userID,
				install.AppID,
				install.InstalledVersion,
				install.AutoUpdate,
				install.Enabled,
				install.InstalledAt,
				install.UpdatedAt,
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func appendAuditEntriesTx(ctx context.Context, tx registryTx, entries []auditEntry) error {
	if len(entries) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for _, entry := range entries {
		metadataJSON := "{}"
		if len(entry.Metadata) > 0 {
			raw, err := json.Marshal(entry.Metadata)
			if err != nil {
				return err
			}
			metadataJSON = string(raw)
		}
		createdAt := entry.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO miniapp_registry_review_audit_log (
				app_id, version, actor_user_id, action, note, metadata_json, created_at
			) VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7)
		`, entry.AppID, zeroToNil(entry.Version), entry.ActorUserID, entry.Action, entry.Note, metadataJSON, createdAt); err != nil {
			return err
		}
	}
	return nil
}

func zeroToNil(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func validateManifest(raw map[string]any) error {
	if len(raw) == 0 {
		return errors.New("manifest_required")
	}
	if stringField(raw, "app_id") == "" {
		return errors.New("app_id required")
	}
	if stringField(raw, "name") == "" {
		return errors.New("name required")
	}
	if !semverPattern.MatchString(stringField(raw, "version")) {
		return errors.New("version must be semver")
	}
	entrypoint, ok := raw["entrypoint"].(map[string]any)
	if !ok {
		return errors.New("entrypoint object required")
	}
	entryType := stringField(entrypoint, "type")
	if entryType != "url" && entryType != "inline" && entryType != "web_bundle" {
		return errors.New("entrypoint.type invalid")
	}
	if stringField(entrypoint, "url") == "" {
		return errors.New("entrypoint.url required")
	}
	preview, ok := raw["message_preview"].(map[string]any)
	if !ok {
		return errors.New("message_preview object required")
	}
	previewType := stringField(preview, "type")
	if previewType != "static_image" && previewType != "live" {
		return errors.New("message_preview.type invalid")
	}
	if stringField(preview, "url") == "" {
		return errors.New("message_preview.url required")
	}
	if _, ok := raw["permissions"].([]any); !ok {
		if _, ok := raw["permissions"].([]string); !ok {
			return errors.New("permissions array required")
		}
	}
	if _, ok := raw["capabilities"].(map[string]any); !ok {
		return errors.New("capabilities object required")
	}
	if sig, ok := raw["signature"]; ok && sig != nil {
		if sigMap, ok := sig.(map[string]any); !ok || stringField(sigMap, "alg") == "" || stringField(sigMap, "kid") == "" || stringField(sigMap, "sig") == "" {
			return errors.New("signature.alg, signature.kid, and signature.sig are required")
		}
	}
	return nil
}

func stringField(m map[string]any, key string) string {
	value, _ := m[key].(string)
	return strings.TrimSpace(value)
}

func manifestHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("%x", sum[:])
}

func manifestOrigin(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

func manifestSourceType(raw map[string]any) string {
	entrypoint, _ := raw["entrypoint"].(map[string]any)
	origin := manifestOrigin(stringField(entrypoint, "url"))
	switch {
	case strings.HasPrefix(origin, "http://localhost:"),
		strings.HasPrefix(origin, "https://localhost:"),
		strings.HasPrefix(origin, "http://127.0.0.1:"),
		strings.HasPrefix(origin, "https://127.0.0.1:"):
		return "dev"
	case origin != "":
		return "external"
	default:
		return "registry"
	}
}

func manifestVisibility(raw map[string]any) string {
	metadata, _ := raw["metadata"].(map[string]any)
	if strings.ToLower(stringField(metadata, "visibility")) == "private" {
		return "private"
	}
	return "public"
}

func manifestSummary(raw map[string]any) string {
	metadata, _ := raw["metadata"].(map[string]any)
	return stringField(metadata, "summary")
}

func manifestPlatforms(raw map[string]any) []string {
	metadata, _ := raw["metadata"].(map[string]any)
	values, _ := metadata["supported_platforms"].([]any)
	out := make([]string, 0, len(values))
	for _, item := range values {
		if value, ok := item.(string); ok && strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	if len(out) == 0 {
		out = []string{"web"}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func initialReviewStatus(sourceType string) string {
	if sourceType == "dev" {
		return statusApproved
	}
	return statusDraft
}

func refreshLatestPointers(app *registeredApp) {
	app.LatestVersion = ""
	app.LatestApproved = ""
	for version, release := range app.Releases {
		if app.LatestVersion == "" || versionGreater(version, app.LatestVersion) {
			app.LatestVersion = version
		}
		if release.ReviewStatus == statusApproved && (app.LatestApproved == "" || versionGreater(version, app.LatestApproved)) {
			app.LatestApproved = version
		}
	}
}

func versionGreater(a, b string) bool {
	if b == "" {
		return true
	}
	parse := func(value string) [3]int {
		var out [3]int
		fmt.Sscanf(strings.SplitN(value, "-", 2)[0], "%d.%d.%d", &out[0], &out[1], &out[2])
		return out
	}
	pa := parse(a)
	pb := parse(b)
	for i := range pa {
		if pa[i] > pb[i] {
			return true
		}
		if pa[i] < pb[i] {
			return false
		}
	}
	return a > b
}

func userInstalls(state *registryState, userID string) map[string]*installedAppRef {
	if strings.TrimSpace(userID) == "" {
		return map[string]*installedAppRef{}
	}
	installs := state.Installs[userID]
	if installs == nil {
		installs = map[string]*installedAppRef{}
		state.Installs[userID] = installs
	}
	return installs
}

func manifestPermissions(raw map[string]any) []string {
	values, ok := raw["permissions"]
	if !ok || values == nil {
		return nil
	}
	switch typed := values.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			value := strings.TrimSpace(item)
			if value != "" {
				out = append(out, value)
			}
		}
		slices.Sort(out)
		return slices.Compact(out)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			value, _ := item.(string)
			value = strings.TrimSpace(value)
			if value != "" {
				out = append(out, value)
			}
		}
		slices.Sort(out)
		return slices.Compact(out)
	default:
		return nil
	}
}

func permissionDelta(installed, candidate *appRelease) (added []string, removed []string) {
	if installed == nil || candidate == nil {
		return nil, nil
	}
	current := manifestPermissions(installed.Manifest)
	next := manifestPermissions(candidate.Manifest)
	currentSet := map[string]struct{}{}
	nextSet := map[string]struct{}{}
	for _, value := range current {
		currentSet[value] = struct{}{}
	}
	for _, value := range next {
		nextSet[value] = struct{}{}
		if _, ok := currentSet[value]; !ok {
			added = append(added, value)
		}
	}
	for _, value := range current {
		if _, ok := nextSet[value]; !ok {
			removed = append(removed, value)
		}
	}
	slices.Sort(added)
	slices.Sort(removed)
	return slices.Compact(added), slices.Compact(removed)
}

func installState(app *registeredApp, install *installedAppRef, updateAvailable bool, updateRequiresConsent bool, release *appRelease) string {
	if release != nil && release.ReviewStatus != statusApproved && release.SourceType != "dev" {
		return "blocked"
	}
	if install == nil {
		return "not_installed"
	}
	if !install.Enabled {
		return "disabled"
	}
	if updateAvailable {
		if updateRequiresConsent {
			return "update_requires_consent"
		}
		return "update_available"
	}
	return "installed"
}

func appResponse(app *registeredApp, release *appRelease, install *installedAppRef) map[string]any {
	var latestApproved *appRelease
	if app.LatestApproved != "" {
		latestApproved = app.Releases[app.LatestApproved]
	}
	var installedRelease *appRelease
	if install != nil && install.InstalledVersion != "" {
		installedRelease = app.Releases[install.InstalledVersion]
	}
	addedPermissions, removedPermissions := permissionDelta(installedRelease, latestApproved)
	updateRequiresConsent := install != nil && len(addedPermissions) > 0
	updateAvailable := install != nil && app.LatestApproved != "" && install.InstalledVersion != "" && install.InstalledVersion != app.LatestApproved
	response := map[string]any{
		"app_id":                  app.AppID,
		"title":                   app.Name,
		"summary":                 app.Summary,
		"version":                 release.Version,
		"visibility":              release.Visibility,
		"review_status":           release.ReviewStatus,
		"source_type":             release.SourceType,
		"publisher_user_id":       release.PublisherUserID,
		"manifest_hash":           release.ManifestHash,
		"entrypoint_origin":       release.EntrypointOrigin,
		"preview_origin":          release.PreviewOrigin,
		"supported_platforms":     release.SupportedPlatforms,
		"manifest":                release.Manifest,
		"created_at":              release.CreatedAt,
		"published_at":            release.PublishedAt,
		"latest_version":          app.LatestVersion,
		"latest_approved_version": app.LatestApproved,
		"permission_delta": map[string]any{
			"added":   addedPermissions,
			"removed": removedPermissions,
		},
		"update_requires_consent": updateRequiresConsent,
	}
	if install == nil {
		response["install"] = map[string]any{
			"installed":   false,
			"auto_update": false,
			"enabled":     false,
		}
		response["update_available"] = false
		response["install_state"] = installState(app, install, false, false, release)
		return response
	}
	response["install"] = map[string]any{
		"installed":         true,
		"installed_version": install.InstalledVersion,
		"auto_update":       install.AutoUpdate,
		"enabled":           install.Enabled,
		"installed_at":      install.InstalledAt,
		"updated_at":        install.UpdatedAt,
	}
	response["update_available"] = updateAvailable
	response["install_state"] = installState(app, install, updateAvailable, updateRequiresConsent, release)
	return response
}
