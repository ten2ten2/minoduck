package platform

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
)

func RandomToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
func TokenHash(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
func credentialCipher(key string) (cipher.AEAD, error) {
	k, e := base64.StdEncoding.DecodeString(key)
	if e != nil || len(k) != 32 {
		return nil, errors.New("ENCRYPTION_NOT_CONFIGURED")
	}
	block, e := aes.NewCipher(k)
	if e != nil {
		return nil, e
	}
	return cipher.NewGCMWithRandomNonce(block)
}
func Encrypt(key, plain, aad string) ([]byte, error) {
	g, e := credentialCipher(key)
	if e != nil {
		return nil, e
	}
	return g.Seal(nil, nil, []byte(plain), []byte(aad)), nil
}
func Decrypt(key string, data []byte, aad string) (string, error) {
	g, e := credentialCipher(key)
	if e != nil {
		return "", e
	}
	if len(data) < g.Overhead() {
		return "", errors.New("INVALID_CIPHERTEXT")
	}
	b, e := g.Open(nil, nil, data, []byte(aad))
	return string(b), e
}
