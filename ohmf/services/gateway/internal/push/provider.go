package push

import "context"

// DeliveryResult represents the result of sending a notification to a single device
type DeliveryResult struct {
	Token          string // Device token
	Success        bool
	MessageID      string // ID from push service
	Error          string
	RetryableError bool
	Permanent      bool // True if token should be removed permanently
}

// NotificationPayload represents a notification to be sent
type NotificationPayload struct {
	Title          string         `json:"title"`
	Body           string         `json:"body"`
	ConversationID string         `json:"conversation_id,omitempty"`
	Data           map[string]any `json:"data,omitempty"`
}

// Provider sends notifications to devices via a push service
type Provider interface {
	// SendNotification sends a notification to a list of device tokens
	// Returns a map of token -> delivery result
	SendNotification(ctx context.Context, tokens []string, payload *NotificationPayload) (map[string]DeliveryResult, error)

	// Close closes the provider's connections
	Close() error
}
