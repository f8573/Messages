package push

import (
	"context"
	"fmt"
	"strings"

	"firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"google.golang.org/api/option"
)

// FCMProvider sends notifications via Firebase Cloud Messaging
type FCMProvider struct {
	client *messaging.Client
	app    *firebase.App
}

// NewFCMProvider creates a new FCM provider initialized with credentials
func NewFCMProvider(ctx context.Context, credentialsPath string) (*FCMProvider, error) {
	opt := option.WithCredentialsFile(credentialsPath)
	app, err := firebase.NewApp(ctx, nil, opt)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Firebase app: %w", err)
	}

	client, err := app.Messaging(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize FCM client: %w", err)
	}

	return &FCMProvider{client: client, app: app}, nil
}

// SendNotification sends a notification to multiple Android devices via FCM
func (p *FCMProvider) SendNotification(ctx context.Context, tokens []string, payload *NotificationPayload) (map[string]DeliveryResult, error) {
	if len(tokens) == 0 {
		return make(map[string]DeliveryResult), nil
	}

	results := make(map[string]DeliveryResult)

	// Build the multicast message
	message := &messaging.MulticastMessage{
		Notification: &messaging.Notification{
			Title: payload.Title,
			Body:  payload.Body,
		},
		Data:   stringifyData(payload),
		Tokens: tokens,
	}

	// Send via multicast to all tokens.
	response, err := p.client.SendEachForMulticast(ctx, message)
	if err != nil {
		// If the entire request failed, mark all tokens as retryable
		for _, token := range tokens {
			results[token] = DeliveryResult{
				Token:          token,
				Success:        false,
				Error:          err.Error(),
				RetryableError: true,
				Permanent:      false,
			}
		}
		return results, err
	}

	// Process per-token results
	for i, token := range tokens {
		if i >= len(response.Responses) {
			results[token] = DeliveryResult{
				Token:   token,
				Success: false,
				Error:   "no response received",
			}
			continue
		}

		resp := response.Responses[i]
		if resp.Success {
			results[token] = DeliveryResult{
				Token:     token,
				Success:   true,
				MessageID: resp.MessageID,
			}
		} else {
			// Determine if error is retryable or permanent
			errMsg := resp.Error.Error()
			retryable := contains(errMsg, "Temporary error", "internal error", "unavailable")
			permanent := contains(errMsg, "InvalidArgument", "NotFound", "InstanceIDError")

			results[token] = DeliveryResult{
				Token:          token,
				Success:        false,
				Error:          errMsg,
				RetryableError: retryable,
				Permanent:      permanent,
			}
		}
	}

	return results, nil
}

// Close closes the FCM client
func (p *FCMProvider) Close() error {
	return nil
}

func contains(s string, substrs ...string) bool {
	for _, substr := range substrs {
		if len(substr) > 0 && len(s) >= len(substr) {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
		}
	}
	return false
}

func stringifyData(payload *NotificationPayload) map[string]string {
	data := make(map[string]string, len(payload.Data)+1)
	for key, value := range payload.Data {
		switch v := value.(type) {
		case string:
			data[key] = v
		case fmt.Stringer:
			data[key] = v.String()
		case nil:
			data[key] = ""
		default:
			data[key] = fmt.Sprint(v)
		}
	}
	if strings.TrimSpace(payload.ConversationID) != "" {
		data["conversation_id"] = payload.ConversationID
	}
	return data
}
