package platform

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestCredentialBoundToWorkspaceAndConnection(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
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
