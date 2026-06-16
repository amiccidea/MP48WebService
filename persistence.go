package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
)

func loadOrGenerateKey(keyPath string) ([]byte, error) {
	if data, err := os.ReadFile(keyPath); err == nil && len(data) == 32 {
		return data, nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, key, 0600); err != nil {
		return nil, err
	}
	log.Println("Generata nuova chiave di crittografia in", keyPath)
	return key, nil
}

func encryptData(data []byte) ([]byte, error) {
	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, data, nil), nil
}

func decryptData(ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

func saveUsers(dataDir string) error {
	data, err := json.Marshal(users)
	if err != nil {
		log.Printf("saveUsers: json.Marshal error: %v", err)
		return err
	}
	encrypted, err := encryptData(data)
	if err != nil {
		log.Printf("saveUsers: encryptData error: %v", err)
		return err
	}
	path := filepath.Join(dataDir, "users.enc")
	err = os.WriteFile(path, encrypted, 0644)
	if err != nil {
		log.Printf("saveUsers: WriteFile error: %v (path=%s)", err, path)
	}
	return err
}

func loadUsers(dataDir string) error {
	encrypted, err := os.ReadFile(filepath.Join(dataDir, "users.enc"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	data, err := decryptData(encrypted)
	if err != nil {
		return err
	}
	var loaded map[string]*User
	if err := json.Unmarshal(data, &loaded); err != nil {
		return err
	}
	userMutex.Lock()
	defer userMutex.Unlock()
	users = loaded
	return nil
}

func saveRoles(dataDir string) error {
	data, err := json.Marshal(roles)
	if err != nil {
		return err
	}
	encrypted, err := encryptData(data)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dataDir, "roles.enc"), encrypted, 0644)
}

func loadRoles(dataDir string) error {
	encrypted, err := os.ReadFile(filepath.Join(dataDir, "roles.enc"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	data, err := decryptData(encrypted)
	if err != nil {
		return err
	}
	var loaded []*Role
	if err := json.Unmarshal(data, &loaded); err != nil {
		return err
	}
	roles = loaded
	return nil
}

func loadRemoteCredentials(dataDir string) (*RemoteCredentials, error) {
	encrypted, err := os.ReadFile(filepath.Join(dataDir, "remote_creds.enc"))
	if os.IsNotExist(err) {
		return nil, nil // file non esiste
	}
	if err != nil {
		return nil, err
	}
	data, err := decryptData(encrypted)
	if err != nil {
		return nil, err
	}
	var creds RemoteCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, err
	}
	return &creds, nil
}

func saveRemoteCredentials(dataDir string, creds *RemoteCredentials) error {
	data, err := json.Marshal(creds)
	if err != nil {
		return err
	}
	encrypted, err := encryptData(data)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dataDir, "remote_creds.enc"), encrypted, 0644)
}
