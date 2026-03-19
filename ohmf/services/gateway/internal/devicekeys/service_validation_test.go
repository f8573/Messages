package devicekeys

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func TestValidatePublishRequestRejectsInvalidSignature(t *testing.T) {
	identityPrivate, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate identity key: %v", err)
	}
	signedPrekeyPrivate, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate signed prekey: %v", err)
	}
	signingPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate signing key: %v", err)
	}
	req := PublishRequest{
		BundleVersion:              BundleVersionSignalV1,
		IdentityKeyAlg:             "X25519",
		IdentityPublicKey:          base64.StdEncoding.EncodeToString(identityPrivate.PublicKey().Bytes()),
		AgreementIdentityPublicKey: base64.StdEncoding.EncodeToString(identityPrivate.PublicKey().Bytes()),
		SigningKeyAlg:              "Ed25519",
		SigningPublicKey:           base64.StdEncoding.EncodeToString(signingPublic),
		SignedPrekeyID:             1,
		SignedPrekeyPublicKey:      base64.StdEncoding.EncodeToString(signedPrekeyPrivate.PublicKey().Bytes()),
		SignedPrekeySignature:      base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize)),
		OneTimePrekeys:             buildValidationPrekeys(t, minInitialOneTimePrekeys),
	}
	if err := validatePublishRequest(req, true); err == nil {
		t.Fatal("expected invalid signature to be rejected")
	}
}

func TestValidatePublishRequestRejectsShortPrekeyBatchOnInitialPublish(t *testing.T) {
	req := buildValidPublishRequest(t)
	req.OneTimePrekeys = req.OneTimePrekeys[:2]
	if err := validatePublishRequest(req, true); err == nil {
		t.Fatal("expected short initial prekey batch to be rejected")
	}
}

func buildValidPublishRequest(t *testing.T) PublishRequest {
	t.Helper()
	identityPrivate, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate identity key: %v", err)
	}
	signedPrekeyPrivate, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate signed prekey: %v", err)
	}
	signingPublic, signingPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate signing key: %v", err)
	}
	signedPrekeyPublic := base64.StdEncoding.EncodeToString(signedPrekeyPrivate.PublicKey().Bytes())
	signature := ed25519.Sign(signingPrivate, []byte(signedPrekeyPayload(BundleVersionSignalV1, 1, signedPrekeyPublic)))
	return PublishRequest{
		BundleVersion:              BundleVersionSignalV1,
		IdentityKeyAlg:             "X25519",
		IdentityPublicKey:          base64.StdEncoding.EncodeToString(identityPrivate.PublicKey().Bytes()),
		AgreementIdentityPublicKey: base64.StdEncoding.EncodeToString(identityPrivate.PublicKey().Bytes()),
		SigningKeyAlg:              "Ed25519",
		SigningPublicKey:           base64.StdEncoding.EncodeToString(signingPublic),
		SignedPrekeyID:             1,
		SignedPrekeyPublicKey:      signedPrekeyPublic,
		SignedPrekeySignature:      base64.StdEncoding.EncodeToString(signature),
		OneTimePrekeys:             buildValidationPrekeys(t, minInitialOneTimePrekeys),
	}
}

func buildValidationPrekeys(t *testing.T, count int) []OneTimePrekey {
	t.Helper()
	prekeys := make([]OneTimePrekey, 0, count)
	for index := 0; index < count; index += 1 {
		privateKey, err := ecdh.X25519().GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("generate one-time prekey %d: %v", index, err)
		}
		prekeys = append(prekeys, OneTimePrekey{
			PrekeyID:  int64(index + 1),
			PublicKey: base64.StdEncoding.EncodeToString(privateKey.PublicKey().Bytes()),
		})
	}
	return prekeys
}
