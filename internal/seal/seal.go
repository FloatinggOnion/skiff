// Package seal encrypts individual secret values before they are stored in the database.
package seal

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"strings"
)

const prefix = "skiff:sealed:v1:"

var aead cipher.AEAD

// Init derives the field key from the instance's recovery key.
func Init(master []byte) error {
	k, err := hkdf.Key(sha256.New, master, nil, "skiff sealed fields v1", 32)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	aead = gcm
	return nil
}

// Seal encrypts a value. An empty value stays empty, so "is this set?" checks keep working.
func Seal(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	if aead == nil {
		return "", errors.New("seal: no key loaded")
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return prefix + base64.RawStdEncoding.EncodeToString(aead.Seal(nonce, nonce, []byte(plain), []byte(prefix))), nil
}

// IsSealed reports whether a stored value is encrypted.
func IsSealed(s string) bool { return strings.HasPrefix(s, prefix) }

// Open returns a stored value's plain form. Values written before encryption pass through unchanged.
func Open(s string) (string, error) {
	if !IsSealed(s) {
		return s, nil
	}
	if aead == nil {
		return "", errors.New("seal: no key loaded")
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(s, prefix))
	if err != nil || len(raw) < aead.NonceSize() {
		return "", errors.New("seal: damaged value")
	}
	n := aead.NonceSize()
	plain, err := aead.Open(nil, raw[:n], raw[n:], []byte(prefix))
	if err != nil {
		return "", fmt.Errorf("seal: wrong key or damaged value: %w", err)
	}
	return string(plain), nil
}

// OpenAll opens each field in place. A field that can't be opened is logged and blanked, so ciphertext never reaches a caller.
func OpenAll(fields ...*string) error {
	var first error
	for _, f := range fields {
		plain, err := Open(*f)
		if err != nil {
			log.Printf("skiff: reading a stored secret: %v", err)
			*f = ""
			if first == nil {
				first = err
			}
			continue
		}
		*f = plain
	}
	return first
}
