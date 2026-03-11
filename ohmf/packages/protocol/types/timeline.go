package types

// UnifiedTimelineItem is the client-facing atomic timeline unit that can represent
// canonical server messages, device-local carrier messages, or mirrored carrier items.
type UnifiedTimelineItem struct {
	MessageID        string                 `json:"message_id"`
	ConversationID   string                 `json:"conversation_id"`
	ServerOrder      int64                  `json:"server_order,omitempty"`
	DisplayTimestamp string                 `json:"display_timestamp"`
	Sender           map[string]string      `json:"sender,omitempty"`
	Transport        string                 `json:"transport"`
	Source           string                 `json:"source"`
	ContentType      string                 `json:"content_type"`
	Content          map[string]interface{} `json:"content"`
	ProviderMetadata map[string]interface{} `json:"provider_metadata,omitempty"`
	VisibilityState  string                 `json:"visibility_state,omitempty"`
}
