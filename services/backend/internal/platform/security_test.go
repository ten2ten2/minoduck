package platform

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestCredentialBoundToWorkspaceAndConnection(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	if !validMasterKey(key) || validMasterKey("not-a-key") || validMasterKey("") {
		t.Fatal("master key validation failed")
	}
	cipher, e := Encrypt(key, "synthetic-provider-key", "workspace-a:account-a")
	if e != nil {
		t.Fatal(e)
	}
	plain, e := Decrypt(key, cipher, "workspace-a:account-a")
	if e != nil || plain != "synthetic-provider-key" {
		t.Fatal("roundtrip failed")
	}
	for _, aad := range []string{"workspace-b:account-a", "workspace-a:account-b"} {
		if _, e = Decrypt(key, cipher, aad); e == nil {
			t.Fatal("cross-tenant credential accepted")
		}
	}
	cipher[len(cipher)-1] ^= 1
	if _, e = Decrypt(key, cipher, "workspace-a:account-a"); e == nil {
		t.Fatal("tampered ciphertext accepted")
	}
}

func TestCredentialRandomNonceAndTruncation(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	first, err := Encrypt(key, "", "workspace:account")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Encrypt(key, "", "workspace:account")
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 28 || bytes.Equal(first, second) {
		t.Fatal("expected a fresh 96-bit nonce and 128-bit tag")
	}
	if plain, err := Decrypt(key, first, "workspace:account"); err != nil || plain != "" {
		t.Fatalf("empty plaintext roundtrip: %q, %v", plain, err)
	}
	for n := range len(first) {
		if _, err := Decrypt(key, first[:n], "workspace:account"); err == nil {
			t.Fatalf("accepted ciphertext truncated to %d bytes", n)
		}
	}
}

func TestCredentialRejectsInvalidKeys(t *testing.T) {
	for _, key := range []string{"", "not-base64", base64.StdEncoding.EncodeToString(make([]byte, 16))} {
		if _, err := Encrypt(key, "secret", "workspace:account"); err == nil {
			t.Fatal("encryption accepted an invalid master key")
		}
		if _, err := Decrypt(key, nil, "workspace:account"); err == nil {
			t.Fatal("decryption accepted an invalid master key")
		}
	}
}

func TestRandomToken(t *testing.T) {
	first, second := RandomToken(), RandomToken()
	decoded, err := base64.RawURLEncoding.DecodeString(first)
	if err != nil || len(decoded) != 32 || first == second {
		t.Fatal("expected distinct 256-bit URL-safe tokens")
	}
}
