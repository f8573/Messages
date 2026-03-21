package push

import (
	"context"
	"fmt"

	"github.com/sideshow/apns2"
	"github.com/sideshow/apns2/certificate"
	apnspayload "github.com/sideshow/apns2/payload"
	apnstoken "github.com/sideshow/apns2/token"
)

// APNsProvider sends notifications via Apple Push Notification service
type APNsProvider struct {
	client   *apns2.Client
	bundleID string
}

// NewAPNsProvider creates a new APNs provider with certificate authentication
func NewAPNsProvider(ctx context.Context, certPath, keyPath, bundleID string) (*APNsProvider, error) {
	_ = ctx
	// Load certificate and key
	cert, err := certificate.FromPemFile(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load APNs certificate: %w", err)
	}

	// Create APNs client (production)
	client := apns2.NewClient(cert).Production()

	return &APNsProvider{
		client:   client,
		bundleID: bundleID,
	}, nil
}

// NewAPNsProviderWithToken creates a new APNs provider with token authentication
// This is the modern approach using .p8 key file
func NewAPNsProviderWithToken(ctx context.Context, keyPath, keyID, teamID, bundleID string) (*APNsProvider, error) {
	_ = ctx
	authKey, err := apnstoken.AuthKeyFromFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load APNs key: %w", err)
	}

	token := &apnstoken.Token{
		AuthKey: authKey,
		KeyID:   keyID,
		TeamID:  teamID,
	}
	client := apns2.NewTokenClient(token).Production()

	return &APNsProvider{
		client:   client,
		bundleID: bundleID,
	}, nil
}

// SendNotification sends a notification to multiple iOS devices via APNs
func (p *APNsProvider) SendNotification(ctx context.Context, tokens []string, payload *NotificationPayload) (map[string]DeliveryResult, error) {
	if len(tokens) == 0 {
		return make(map[string]DeliveryResult), nil
	}

	results := make(map[string]DeliveryResult)

	// Send to each token individually (APNs doesn't support multicast)
	for _, token := range tokens {
		notification := &apns2.Notification{
			DeviceToken: token,
			Topic:       p.bundleID,
			PushType:    apns2.PushTypeAlert,
			Priority:    apns2.PriorityHigh,
		}

		data := make(map[string]interface{}, len(payload.Data)+1)
		for key, value := range payload.Data {
			data[key] = value
		}
		if payload.ConversationID != "" {
			data["conversation_id"] = payload.ConversationID
		}

		builder := apnspayload.NewPayload().
			AlertTitle(payload.Title).
			AlertBody(payload.Body).
			Sound("default")
		if len(data) > 0 {
			builder.Custom("data", data)
		}
		notification.Payload = builder

		res, err := p.client.PushWithContext(ctx, notification)
		if err != nil {
			results[token] = DeliveryResult{
				Token:          token,
				Success:        false,
				Error:          err.Error(),
				RetryableError: true,
				Permanent:      false,
			}
			continue
		}

		if res.Sent() {
			results[token] = DeliveryResult{
				Token:     token,
				Success:   true,
				MessageID: res.ApnsID,
			}
		} else {
			// Determine if error is retryable or permanent
			reason := res.Reason
			retryable := reason == apns2.ReasonServiceUnavailable ||
				reason == apns2.ReasonInternalServerError ||
				reason == apns2.ReasonTooManyRequests ||
				reason == apns2.ReasonShutdown

			permanent := reason == apns2.ReasonBadDeviceToken ||
				reason == apns2.ReasonExpiredToken ||
				reason == apns2.ReasonMissingDeviceToken ||
				reason == apns2.ReasonUnregistered

			results[token] = DeliveryResult{
				Token:          token,
				Success:        false,
				Error:          reason,
				RetryableError: retryable,
				Permanent:      permanent,
			}
		}
	}

	return results, nil
}

// Close closes the APNs client connection
func (p *APNsProvider) Close() error {
	if p.client != nil {
		p.client.CloseIdleConnections()
	}
	return nil
}
