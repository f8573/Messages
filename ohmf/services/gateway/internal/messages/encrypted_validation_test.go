package messages

import "testing"

func TestValidateSendContentRejectsMalformedEncryptedEnvelope(t *testing.T) {
	err := validateSendContent("encrypted", map[string]any{
		"ciphertext": "abc",
		"encryption": map[string]any{
			"scheme":           "OHMF_SIGNAL_V1",
			"sender_user_id":   "user",
			"sender_device_id": "device",
			"sender_signature": "sig",
			"recipients":       []any{},
		},
	})
	if err == nil {
		t.Fatal("expected missing nonce / recipients to be rejected")
	}
}

func TestValidateSendContentAcceptsSignalEnvelope(t *testing.T) {
	err := validateSendContent("encrypted", map[string]any{
		"ciphertext": "cipher",
		"nonce":      "nonce",
		"encryption": map[string]any{
			"scheme":           "OHMF_SIGNAL_V1",
			"sender_user_id":   "user",
			"sender_device_id": "device",
			"sender_signature": "sig",
			"recipients": []any{
				map[string]any{
					"user_id":     "u1",
					"device_id":   "d1",
					"wrapped_key": "wk",
					"wrap_nonce":  "wn",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("expected valid signal envelope, got %v", err)
	}
}
