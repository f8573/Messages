package types

// RelayJob represents a linked-device relay job.
type RelayJob struct {
	RelayJobID       string                 `json:"relay_job_id"`
	RequestedBy      map[string]string      `json:"requested_by"` // user_id, device_id
	ExecutingDeviceID string                `json:"executing_device_id,omitempty"`
	Transport        string                 `json:"transport"`
	Destination      map[string]string      `json:"destination"` // e.g., phone_e164
	Content          map[string]interface{} `json:"content"`
	Status           string                 `json:"status"`
	IdempotencyKey   string                 `json:"idempotency_key,omitempty"`
	CreatedAt        string                 `json:"created_at,omitempty"`
	UpdatedAt        string                 `json:"updated_at,omitempty"`
}
