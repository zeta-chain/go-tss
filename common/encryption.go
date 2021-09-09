package common

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
)

// Encrypt the input data with passphrase
func AESEncrypt(data, derivedKey []byte) ([]byte, error) {
	if len(data) == 0 || len(derivedKey) == 0 {
		return nil, errors.New("invalid data or derivedkey")
	}

	block, _ := aes.NewCipher(derivedKey)
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nonce, nonce, data, nil)
	return ciphertext, nil
}

// AESDecrypt the input data with passphrase
func AESDecrypt(data, derivedKey []byte) ([]byte, error) {
	if len(data) == 0 || len(derivedKey) == 0 {
		return nil, errors.New("invalid data or derivedkey")
	}

	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}
	return plaintext, nil
}
