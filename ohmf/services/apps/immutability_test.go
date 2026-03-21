package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"
)

// TestComputeManifestContentHash verifies deterministic hash computation
func TestComputeManifestContentHash(t *testing.T) {
	manifest := map[string]any{
		"app_id":    "test-app",
		"version":   "1.0.0",
		"name":      "Test App",
		"platforms": []string{"web", "android"},
	}

	// Same manifest should always produce same hash
	bytes1, _ := json.Marshal(manifest)
	hash1 := computeManifestContentHash(bytes1)

	bytes2, _ := json.Marshal(manifest)
	hash2 := computeManifestContentHash(bytes2)

	if hash1 != hash2 {
		t.Errorf("Hash mismatch for identical manifest: %s vs %s", hash1, hash2)
	}

	// Verify hash format is correct
	if len(hash1) < 10 {
		t.Errorf("Hash too short: %s", hash1)
	}

	// Verify it's SHA-256 (64 hex chars + "sha256:" prefix)
	expected := fmt.Sprintf("sha256:%x", sha256.Sum256(bytes1))
	if hash1 != expected {
		t.Errorf("Hash format mismatch: expected %s, got %s", expected, hash1)
	}
}

// TestComputeAssetSetHash verifies asset hash aggregation
func TestComputeAssetSetHash(t *testing.T) {
	assets := []string{
		"sha256:abc123def456",
		"sha256:xyz789uvw012",
		"sha256:pqr345stu678",
	}

	hash := computeAssetSetHash(assets)

	// Verify hash format
	if len(hash) < 10 {
		t.Errorf("Asset set hash too short: %s", hash)
	}

	// Same assets in same order should produce same hash
	hash2 := computeAssetSetHash(assets)
	if hash != hash2 {
		t.Errorf("Asset set hash mismatch: %s vs %s", hash, hash2)
	}

	// Verify it includes all assets
	concatenated := ""
	for _, a := range assets {
		concatenated += a
	}
	expected := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(concatenated)))
	if hash != expected {
		t.Errorf("Asset set hash mismatch with expected: %s vs %s", hash, expected)
	}
}

// TestValidateManifestImmutability verifies immutability enforcement
func TestValidateManifestImmutability(t *testing.T) {
	tests := []struct {
		name        string
		release     *appRelease
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid_unchanged_manifest",
			release: func() *appRelease {
				manifest := map[string]any{
					"app_id":  "test-app",
					"version": "1.0.0",
					"name":    "Test App",
				}
				bytes, _ := json.Marshal(manifest)
				hash := computeManifestContentHash(bytes)
				return &appRelease{
					Manifest:            manifest,
					ManifestContentHash: hash,
				}
			}(),
			expectError: false,
		},
		{
			name: "legacy_release_no_hash",
			release: &appRelease{
				Manifest: map[string]any{
					"app_id":  "test-app",
					"version": "1.0.0",
				},
				ManifestContentHash: "",
			},
			expectError: false, // Legacy releases allowed
		},
		{
			name: "manifest_changed_after_creation",
			release: func() *appRelease {
				manifest := map[string]any{
					"app_id":  "test-app",
					"version": "1.0.0",
					"name":    "Test App",
				}
				bytes, _ := json.Marshal(manifest)
				hash := computeManifestContentHash(bytes)

				// Tamper with manifest after hash
				manifest["name"] = "Malicious App"

				return &appRelease{
					Manifest:            manifest,
					ManifestContentHash: hash,
				}
			}(),
			expectError: true,
			errorMsg:    "manifest_has_changed_after_creation",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateManifestImmutability(tc.release)
			if tc.expectError && err == nil {
				t.Errorf("Expected error but got nil")
			}
			if !tc.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
			if tc.expectError && tc.errorMsg != "" && err != nil {
				if !contains(err.Error(), tc.errorMsg) {
					t.Errorf("Error message mismatch. Expected to contain '%s', got '%s'", tc.errorMsg, err.Error())
				}
			}
		})
	}
}

// TestImmutabilityHashUniqueness verifies different manifests produce different hashes
func TestImmutabilityHashUniqueness(t *testing.T) {
	manifest1 := map[string]any{
		"app_id":  "app1",
		"version": "1.0.0",
		"name":    "App One",
	}
	bytes1, _ := json.Marshal(manifest1)
	hash1 := computeManifestContentHash(bytes1)

	manifest2 := map[string]any{
		"app_id":  "app2",
		"version": "1.0.1",
		"name":    "App Two",
	}
	bytes2, _ := json.Marshal(manifest2)
	hash2 := computeManifestContentHash(bytes2)

	if hash1 == hash2 {
		t.Errorf("Different manifests should produce different hashes, but both produced %s", hash1)
	}
}

// Helper function for string contains
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
