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
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func TokenHash(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
func Encrypt(key, plain, aad string) ([]byte, error) {
	k, e := base64.StdEncoding.DecodeString(key)
	if e != nil || len(k) != 32 {
		return nil, errors.New("ENCRYPTION_NOT_CONFIGURED")
	}
	block, e := aes.NewCipher(k)
	if e != nil {
		return nil, e
	}
	g, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	nonce := make([]byte, g.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return nil, e
	}
	return g.Seal(nonce, nonce, []byte(plain), []byte(aad)), nil
}
func Decrypt(key string, data []byte, aad string) (string, error) {
	k, e := base64.StdEncoding.DecodeString(key)
	if e != nil || len(k) != 32 {
		return "", errors.New("ENCRYPTION_NOT_CONFIGURED")
	}
	block, e := aes.NewCipher(k)
	if e != nil {
		return "", e
	}
	g, e := cipher.NewGCM(block)
	if e != nil {
		return "", e
	}
	if len(data) < g.NonceSize() {
		return "", errors.New("INVALID_CIPHERTEXT")
	}
	b, e := g.Open(nil, data[:g.NonceSize()], data[g.NonceSize():], []byte(aad))
	return string(b), e
}
