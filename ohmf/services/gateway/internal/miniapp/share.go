package miniapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"ohmf/services/gateway/internal/replication"
)

const (
	contentTypeAppCard = "app_card"
	miniAppCapability  = "MINI_APPS"
)

var (
	ErrMiniAppUnsupported = errors.New("miniapp_unsupported")
	ErrMiniAppConsent     = errors.New("miniapp_consent_required")
)

type ShareInput struct {
	ManifestID         string
	AppID              string
	ConversationID     string
	GrantedPermissions []string
	StateSnapshot      any
	ResumeExisting     bool
}

func (s *Service) CreateSessionForUser(ctx context.Context, userID string, input CreateSessionInput) (map[string]any, bool, error) {
	if input.Viewer.UserID == "" {
		input.Viewer.UserID = userID
	}
	record, created, err := s.CreateSession(ctx, input)
	if err != nil {
		return nil, created, err
	}
	sessionID, _ := record["app_session_id"].(string)
	if sessionID == "" {
		return nil, created, fmt.Errorf("app_session_id missing")
	}
	if _, err := s.JoinSession(ctx, userID, sessionID, input.GrantedPermissions); err != nil {
		return nil, created, err
	}
	record, err = s.GetSessionForUser(ctx, userID, sessionID)
	return record, created, err
}

func (s *Service) GetSessionForUser(ctx context.Context, userID, sessionID string) (map[string]any, error) {
	record, err := s.loadSessionRecord(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if err := s.ensureConversationMember(ctx, userID, record.ConversationID); err != nil {
		return nil, err
	}
	permitted, err := s.manifestPermissionsForAppID(ctx, record.AppID)
	if err != nil {
		return nil, err
	}
	return s.sessionRecordToMapForUser(record, userID, permitted), nil
}

func (s *Service) JoinSession(ctx context.Context, userID, sessionID string, grantedPermissions []string) (map[string]any, error) {
	record, err := s.loadSessionRecord(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if record.EndedAt != nil {
		return nil, ErrSessionEnded
	}
	if err := s.ensureConversationMember(ctx, userID, record.ConversationID); err != nil {
		return nil, err
	}
	permitted, err := s.manifestPermissionsForAppID(ctx, record.AppID)
	if err != nil {
		return nil, err
	}
	normalized := sanitizeGrantedPermissions(grantedPermissions, permitted)
	if grantedPermissions == nil {
		normalized = append([]string(nil), permitted...)
	}
	participantPermissions := cloneParticipantPermissions(record.ParticipantPermissions)
	participantPermissions[userID] = normalized
	participants := ensureParticipant(record.Participants, SessionParticipant{UserID: userID, Role: "PLAYER"})
	partsB, _ := json.Marshal(participants)
	permsB, _ := json.Marshal(participantPermissions)
	if _, err := s.db.Exec(
		ctx,
		`UPDATE miniapp_sessions
		    SET participants = $1::jsonb,
		        participant_permissions = $2::jsonb
		  WHERE id = $3::uuid`,
		string(partsB),
		string(permsB),
		sessionID,
	); err != nil {
		return nil, err
	}

	// P4.1 Event Model: Log session_joined event with participant and permissions
	participant := SessionParticipant{UserID: userID, Role: "PLAYER"}
	_ = s.logSessionJoined(ctx, sessionID, userID, participant, normalized)

	record.Participants = participants
	record.ParticipantPermissions = participantPermissions
	return s.sessionRecordToMapForUser(record, userID, permitted), nil
}

func (s *Service) EndSessionForUser(ctx context.Context, userID, sessionID string) error {
	record, err := s.loadSessionRecord(ctx, sessionID)
	if err != nil {
		return err
	}
	if record.EndedAt != nil {
		return ErrSessionEnded
	}
	if err := s.ensureConversationMember(ctx, userID, record.ConversationID); err != nil {
		return err
	}
	return s.EndSession(ctx, sessionID)
}

func (s *Service) AppendEventForUser(ctx context.Context, userID, sessionID, eventName string, body any) (int64, error) {
	record, err := s.loadSessionRecord(ctx, sessionID)
	if err != nil {
		return 0, err
	}
	if record.EndedAt != nil {
		return 0, ErrSessionEnded
	}
	if err := s.ensureConversationMember(ctx, userID, record.ConversationID); err != nil {
		return 0, err
	}
	if !record.hasJoined(userID) {
		return 0, ErrMiniAppConsent
	}

	// P1.2: Capability Enforcement Layer - validate bridge method against granted capabilities
	grantedCapabilities := record.viewerGrantedPermissions(userID)
	if err := validateBridgeMethodWithRateLimit(sessionID, grantedCapabilities, eventName); err != nil {
		// Log denied capability call for security audit
		_ = s.auditLogCapabilityCheck(ctx, userID, sessionID, eventName, false, err.Error())

		// Return generic error to prevent fingerprinting
		if errors.Is(err, ErrBridgeMethodRateLimited) {
			return 0, ErrBridgeMethodRateLimited
		}
		return 0, ErrBridgeMethodNotAllowed
	}

	// Log allowed capability call for audit trail
	_ = s.auditLogCapabilityCheck(ctx, userID, sessionID, eventName, true, "")

	return s.AppendEvent(ctx, sessionID, userID, eventName, body)
}

func (s *Service) SnapshotSessionForUser(ctx context.Context, userID, sessionID string, state any, version int) (int, error) {
	record, err := s.loadSessionRecord(ctx, sessionID)
	if err != nil {
		return 0, err
	}
	if record.EndedAt != nil {
		return 0, ErrSessionEnded
	}
	if err := s.ensureConversationMember(ctx, userID, record.ConversationID); err != nil {
		return 0, err
	}
	if !record.hasJoined(userID) {
		return 0, ErrMiniAppConsent
	}
	return s.SnapshotSession(ctx, sessionID, state, version, userID)
}

func (s *Service) ShareSession(ctx context.Context, userID string, input ShareInput) (map[string]any, error) {
	if input.ConversationID == "" {
		return nil, fmt.Errorf("conversation_id required")
	}
	resumeExisting := true
	if !input.ResumeExisting {
		resumeExisting = false
	}

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if err := s.ensureConversationMemberTx(ctx, tx, userID, input.ConversationID); err != nil {
		return nil, err
	}
	participants, err := s.loadConversationParticipantsTx(ctx, tx, input.ConversationID)
	if err != nil {
		return nil, err
	}
	if err := s.ensureMiniAppEligibleTx(ctx, tx, input.ConversationID, participants); err != nil {
		return nil, err
	}

	manifestID, appID, permitted, err := s.resolveManifest(ctx, input.ManifestID, input.AppID)
	if err != nil {
		return nil, err
	}
	granted := sanitizeGrantedPermissions(input.GrantedPermissions, permitted)
	if len(granted) == 0 {
		granted = append([]string(nil), permitted...)
	}

	var record sessionRecord
	if resumeExisting {
		existing, err := s.findActiveSessionTx(ctx, tx, appID, input.ConversationID)
		if err != nil && !errors.Is(err, ErrSessionNotFound) {
			return nil, err
		}
		if err == nil {
			record = *existing
			record.Participants = normalizeParticipants(participants, SessionParticipant{})
			record.ParticipantPermissions = cloneParticipantPermissions(record.ParticipantPermissions)
			record.ParticipantPermissions[userID] = granted
			partsB, _ := json.Marshal(record.Participants)
			partPermsB, _ := json.Marshal(record.ParticipantPermissions)
			if _, err := tx.Exec(
				ctx,
				`UPDATE miniapp_sessions
				    SET participants = $1::jsonb,
				        participant_permissions = $2::jsonb
				  WHERE id = $3::uuid`,
				string(partsB),
				string(partPermsB),
				record.ID,
			); err != nil {
				return nil, err
			}
			record.Participants = normalizeParticipants(record.Participants, SessionParticipant{})
		}
	}

	if record.ID == "" {
		id, err := randomSessionID(ctx, tx)
		if err != nil {
			return nil, err
		}
		record = sessionRecord{
			ID:                     id,
			ManifestID:             manifestID,
			AppID:                  appID,
			ConversationID:         input.ConversationID,
			Participants:           normalizeParticipants(participants, SessionParticipant{}),
			GlobalPermissions:      granted,
			ParticipantPermissions: map[string][]string{userID: granted},
			State:                  defaultSessionState(input.StateSnapshot),
			StateVersion:           1,
			CreatedBy:              userID,
		}
		partsB, _ := json.Marshal(record.Participants)
		permsB, _ := json.Marshal(record.GlobalPermissions)
		partPermsB, _ := json.Marshal(record.ParticipantPermissions)
		stateB, _ := json.Marshal(record.State)
		if _, err := tx.Exec(
			ctx,
			`INSERT INTO miniapp_sessions (id, manifest_id, app_id, conversation_id, participants, granted_permissions, participant_permissions, state, state_version, created_by, created_at)
			 VALUES ($1::uuid, $2::uuid, $3, $4::uuid, $5::jsonb, $6::jsonb, $7::jsonb, $8::jsonb, 1, $9::uuid, now())`,
			record.ID,
			record.ManifestID,
			record.AppID,
			record.ConversationID,
			string(partsB),
			string(permsB),
			string(partPermsB),
			string(stateB),
			record.CreatedBy,
		); err != nil {
			return nil, err
		}
	}

	entry, err := s.loadCatalogEntryByAppID(ctx, "", appID)
	if err != nil {
		return nil, err
	}
	manifestRow := catalogEntryToMap(entry)
	cardContent := buildAppCardContent(manifestRow, record)
	message, err := s.insertAppCardMessageTx(ctx, tx, userID, input.ConversationID, cardContent)
	if err != nil {
		return nil, err
	}
	recipients := conversationRecipients(participants, userID)
	if s.replication != nil {
		conversationMeta, err := s.replication.LoadConversationMeta(ctx, tx, input.ConversationID)
		if err != nil {
			return nil, err
		}
		if err := s.replication.AppendDomainEvent(ctx, tx, input.ConversationID, userID, replication.DomainEventMessageCreated, replication.MessageCreatedPayload{
			MessageID:         stringField(message, "message_id"),
			ConversationID:    input.ConversationID,
			ConversationType:  conversationMeta.Type,
			SenderUserID:      userID,
			ContentType:       contentTypeAppCard,
			Content:           cardContent,
			ClientGeneratedID: "",
			Transport:         "OTT",
			ServerOrder:       int64Field(message, "server_order"),
			CreatedAt:         stringField(message, "created_at"),
			Participants:      conversationMeta.Participants,
			ExternalPhones:    conversationMeta.ExternalPhones,
		}); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	s.publishMessageCreated(ctx, recipients, map[string]any{
		"message_id":        stringField(message, "message_id"),
		"conversation_id":   input.ConversationID,
		"sender_user_id":    userID,
		"content_type":      contentTypeAppCard,
		"content":           cardContent,
		"transport":         "OTT",
		"server_order":      int64Field(message, "server_order"),
		"created_at":        stringField(message, "created_at"),
		"status":            "SENT",
		"sent_at":           stringField(message, "created_at"),
		"status_updated_at": stringField(message, "created_at"),
	})
	return map[string]any{
		"message":              message,
		"app_session_id":       record.ID,
		"manifest_id":          record.ManifestID,
		"app_id":               record.AppID,
		"capabilities_granted": granted,
		"launch_context":       buildLaunchContextForUser(record, userID, permitted),
		"state":                record.State,
		"state_version":        record.StateVersion,
	}, nil
}

func (s *Service) ensureConversationMember(ctx context.Context, userID, conversationID string) error {
	return s.ensureConversationMemberTx(ctx, s.db, userID, conversationID)
}

type miniappQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func (s *Service) ensureConversationMemberTx(ctx context.Context, q miniappQuerier, userID, conversationID string) error {
	var exists bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversation_members WHERE conversation_id = $1::uuid AND user_id = $2::uuid)`, conversationID, userID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("conversation_access_denied")
	}
	return nil
}

func (s *Service) loadConversationParticipantsTx(ctx context.Context, q miniappQuerier, conversationID string) ([]SessionParticipant, error) {
	rows, err := q.Query(ctx, `
		SELECT cm.user_id::text, COALESCE(NULLIF(u.display_name, ''), u.primary_phone_e164, cm.user_id::text)
		FROM conversation_members cm
		JOIN users u ON u.id = cm.user_id
		WHERE cm.conversation_id = $1::uuid
		ORDER BY cm.joined_at
	`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var participants []SessionParticipant
	for rows.Next() {
		var userID, displayName string
		if err := rows.Scan(&userID, &displayName); err != nil {
			return nil, err
		}
		participants = append(participants, SessionParticipant{UserID: userID, Role: "PLAYER", DisplayName: displayName})
	}
	return normalizeParticipants(participants, SessionParticipant{}), rows.Err()
}

func (s *Service) ensureMiniAppEligibleTx(ctx context.Context, q miniappQuerier, conversationID string, participants []SessionParticipant) error {
	for _, participant := range participants {
		var supported bool
		if err := q.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1
				FROM devices
				WHERE user_id = $1::uuid
				  AND ($2 = ANY(capabilities))
			)
		`, participant.UserID, miniAppCapability).Scan(&supported); err != nil {
			return err
		}
		if !supported {
			return ErrMiniAppUnsupported
		}
	}
	return nil
}

func (s *Service) findActiveSessionTx(ctx context.Context, q miniappQuerier, appID, conversationID string) (*sessionRecord, error) {
	var record sessionRecord
	var participantsB, permissionsB, participantPermissionsB, stateB []byte
	var createdBy *string
	err := q.QueryRow(
		ctx,
		`SELECT id::text, manifest_id::text, app_id, conversation_id::text, participants, granted_permissions, participant_permissions, state, state_version, created_by::text, expires_at, created_at, ended_at
		   FROM miniapp_sessions
		  WHERE app_id = $1 AND conversation_id = $2::uuid AND ended_at IS NULL
		  ORDER BY created_at DESC
		  LIMIT 1`,
		appID,
		conversationID,
	).Scan(
		&record.ID,
		&record.ManifestID,
		&record.AppID,
		&record.ConversationID,
		&participantsB,
		&permissionsB,
		&participantPermissionsB,
		&stateB,
		&record.StateVersion,
		&createdBy,
		&record.ExpiresAt,
		&record.CreatedAt,
		&record.EndedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	if createdBy != nil {
		record.CreatedBy = *createdBy
	}
	record.Participants = decodeParticipants(participantsB)
	record.GlobalPermissions = decodeStringList(permissionsB)
	record.ParticipantPermissions = decodeParticipantPermissions(participantPermissionsB)
	record.State = decodeSessionState(stateB)
	return &record, nil
}

func (s *Service) insertAppCardMessageTx(ctx context.Context, tx pgx.Tx, userID, conversationID string, content map[string]any) (map[string]any, error) {
	var next int64
	if err := tx.QueryRow(ctx, `
		UPDATE conversation_counters
		SET next_server_order = next_server_order + 1, updated_at = now()
		WHERE conversation_id = $1::uuid
		RETURNING next_server_order - 1
	`, conversationID).Scan(&next); err != nil {
		return nil, err
	}
	contentJSON, err := json.Marshal(content)
	if err != nil {
		return nil, err
	}
	var messageID string
	var createdAt time.Time
	if err := tx.QueryRow(ctx, `
		INSERT INTO messages (conversation_id, sender_user_id, content_type, content, transport, server_order)
		VALUES ($1::uuid, $2::uuid, $3, $4::jsonb, 'OTT', $5)
		RETURNING id::text, created_at
	`, conversationID, userID, contentTypeAppCard, string(contentJSON), next).Scan(&messageID, &createdAt); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE conversations
		SET last_message_id = $2::uuid, updated_at = now()
		WHERE id = $1::uuid
	`, conversationID, messageID); err != nil {
		return nil, err
	}
	return map[string]any{
		"message_id":      messageID,
		"conversation_id": conversationID,
		"sender_user_id":  userID,
		"content_type":    contentTypeAppCard,
		"content":         content,
		"transport":       "OHMF",
		"server_order":    next,
		"created_at":      createdAt.UTC().Format(time.RFC3339),
		"status":          "SENT",
	}, nil
}

func buildAppCardContent(manifestRow map[string]any, record sessionRecord) map[string]any {
	manifest, _ := manifestRow["manifest"].(map[string]any)
	title := stringField(manifest, "name")
	if title == "" {
		title = record.AppID
	}
	return map[string]any{
		"card_version":    1,
		"share_mode":      "shared_session",
		"app_id":          record.AppID,
		"manifest_id":     record.ManifestID,
		"app_session_id":  record.ID,
		"title":           title,
		"summary":         buildAppCardSummary(manifest, title),
		"icon_url":        iconURLFromManifest(manifest),
		"message_preview": buildAppCardPreview(manifest),
		"preview_state":   cloneMap(record.State.Snapshot),
		"cta_label":       "Open",
		"joinable":        true,
	}
}

func buildAppCardSummary(manifest map[string]any, title string) string {
	metadata, _ := manifest["metadata"].(map[string]any)
	if metadata != nil {
		if summary, ok := metadata["summary"].(string); ok && summary != "" {
			return summary
		}
		if description, ok := metadata["description"].(string); ok && description != "" {
			return description
		}
	}
	return fmt.Sprintf("Open %s in this conversation.", title)
}

func iconURLFromManifest(manifest map[string]any) string {
	rawIcons, _ := manifest["icons"].([]any)
	for _, raw := range rawIcons {
		icon, _ := raw.(map[string]any)
		if icon == nil {
			continue
		}
		if src := stringField(icon, "url"); src != "" {
			return src
		}
		if src := stringField(icon, "src"); src != "" {
			return src
		}
	}
	return ""
}

func buildAppCardPreview(manifest map[string]any) map[string]any {
	rawPreview, _ := manifest["message_preview"].(map[string]any)
	if rawPreview == nil {
		return nil
	}

	previewType := stringField(rawPreview, "type")
	previewURL := stringField(rawPreview, "url")
	if previewType == "" || previewURL == "" {
		return nil
	}

	preview := map[string]any{
		"type": previewType,
		"url":  previewURL,
	}
	if altText := stringField(rawPreview, "alt_text"); altText != "" {
		preview["alt_text"] = altText
	}
	fitMode := stringField(rawPreview, "fit_mode")
	if fitMode == "" {
		fitMode = "scale"
	}
	preview["fit_mode"] = fitMode
	return preview
}

func buildLaunchContextForUser(record sessionRecord, userID string, permitted []string) map[string]any {
	return map[string]any{
		"app_id":               record.AppID,
		"app_session_id":       record.ID,
		"conversation_id":      record.ConversationID,
		"viewer":               viewerForUser(record.Participants, userID),
		"participants":         record.Participants,
		"capabilities_granted": record.viewerGrantedPermissions(userID),
		"state_snapshot":       record.State.Snapshot,
		"state_version":        record.StateVersion,
		"consent_required":     len(permitted) > 0 && !record.hasJoined(userID),
		"joinable":             record.EndedAt == nil,
	}
}

func cloneMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func conversationRecipients(participants []SessionParticipant, senderUserID string) []string {
	recipients := make([]string, 0, len(participants))
	for _, participant := range participants {
		if participant.UserID == "" || participant.UserID == senderUserID {
			continue
		}
		recipients = append(recipients, participant.UserID)
	}
	return recipients
}

func (s *Service) publishMessageCreated(ctx context.Context, recipients []string, payload map[string]any) {
	if s.redis == nil || len(recipients) == 0 {
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	for _, recipientID := range recipients {
		if recipientID == "" {
			continue
		}
		_ = s.redis.Publish(ctx, "message:user:"+recipientID, body).Err()
	}
}

func int64Field(payload map[string]any, key string) int64 {
	switch value := payload[key].(type) {
	case int64:
		return value
	case int32:
		return int64(value)
	case int:
		return int64(value)
	case float64:
		return int64(value)
	default:
		return 0
	}
}

func (s *Service) sessionRecordToMapForUser(record sessionRecord, userID string, permitted []string) map[string]any {
	return map[string]any{
		"app_session_id":          record.ID,
		"manifest_id":             record.ManifestID,
		"app_id":                  record.AppID,
		"conversation_id":         record.ConversationID,
		"viewer":                  viewerForUser(record.Participants, userID),
		"participants":            record.Participants,
		"capabilities_granted":    record.viewerGrantedPermissions(userID),
		"participant_permissions": record.ParticipantPermissions,
		"state":                   record.State,
		"state_version":           record.StateVersion,
		"expires_at":              record.ExpiresAt,
		"created_at":              record.CreatedAt,
		"launch_context":          buildLaunchContextForUser(record, userID, permitted),
		"consent_required":        len(permitted) > 0 && !record.hasJoined(userID),
		"joinable":                record.EndedAt == nil,
		"ended_at":                record.EndedAt,
	}
}

func ensureParticipant(participants []SessionParticipant, viewer SessionParticipant) []SessionParticipant {
	return normalizeParticipants(participants, viewer)
}

func viewerForUser(participants []SessionParticipant, userID string) SessionParticipant {
	for _, participant := range participants {
		if participant.UserID == userID {
			return participant
		}
	}
	return SessionParticipant{UserID: userID, Role: "PLAYER"}
}

func cloneParticipantPermissions(value map[string][]string) map[string][]string {
	if len(value) == 0 {
		return map[string][]string{}
	}
	out := make(map[string][]string, len(value))
	for userID, permissions := range value {
		out[userID] = append([]string(nil), permissions...)
	}
	return out
}

func decodeParticipantPermissions(raw []byte) map[string][]string {
	if len(raw) == 0 {
		return map[string][]string{}
	}
	var value map[string][]string
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return map[string][]string{}
	}
	for userID, permissions := range value {
		slices.Sort(permissions)
		value[userID] = slices.Compact(permissions)
	}
	return value
}

func (record sessionRecord) viewerGrantedPermissions(userID string) []string {
	if permissions, ok := record.ParticipantPermissions[userID]; ok {
		return append([]string(nil), permissions...)
	}
	if len(record.ParticipantPermissions) == 0 {
		return append([]string(nil), record.GlobalPermissions...)
	}
	return nil
}

func (record sessionRecord) hasJoined(userID string) bool {
	if _, ok := record.ParticipantPermissions[userID]; ok {
		return true
	}
	return len(record.ParticipantPermissions) == 0 && userID == record.CreatedBy
}

func randomSessionID(ctx context.Context, tx pgx.Tx) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&id)
	return id, err
}
