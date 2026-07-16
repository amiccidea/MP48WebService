package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
)

func loadOrGenerateKey(keyPath string) ([]byte, error) {
	log.Printf("🔑 Tentativo di caricare chiave da: %s", keyPath)
	if data, err := ioutil.ReadFile(keyPath); err == nil && len(data) == 32 {
		log.Printf("✅ Chiave caricata con successo (len=%d)", len(data))
		return data, nil
	} else if err != nil {
		log.Printf("⚠️ Errore lettura chiave: %v", err)
	}
	log.Printf("🆕 Generazione nuova chiave in %s", keyPath)
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		log.Printf("❌ Errore generazione chiave: %v", err)
		return nil, err
	}
	if err := ioutil.WriteFile(keyPath, key, 0600); err != nil {
		log.Printf("❌ Errore scrittura chiave: %v", err)
		return nil, err
	}
	log.Printf("✅ Nuova chiave generata e salvata in %s", keyPath)
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
	log.Printf("🔐 Decifratura dati (len=%d)", len(ciphertext))
	if len(encryptionKey) == 0 {
		log.Printf("❌ encryptionKey è vuoto! Impossibile decifrare.")
		return nil, errors.New("encryption key is empty")
	}
	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		log.Printf("❌ Errore AES: %v", err)
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		log.Printf("❌ Errore GCM: %v", err)
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		log.Printf("❌ Ciphertext troppo corto: %d < %d", len(ciphertext), nonceSize)
		return nil, errors.New("ciphertext too short")
	}
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		log.Printf("❌ Errore decifratura: %v", err)
		return nil, err
	}
	log.Printf("✅ Decifratura riuscita (len=%d)", len(plaintext))
	return plaintext, nil
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
	err = ioutil.WriteFile(path, encrypted, 0644)
	if err != nil {
		log.Printf("saveUsers: WriteFile error: %v (path=%s)", err, path)
	}
	return err
}

func loadUsers(dataDir string) error {
	path := filepath.Join(dataDir, "users.enc")
	log.Printf("📂 Caricamento utenti da %s", path)
	encrypted, err := ioutil.ReadFile(path)
	if os.IsNotExist(err) {
		log.Printf("ℹ️ File users.enc non esiste, verrà creato al primo salvataggio")
		return nil
	}
	if err != nil {
		log.Printf("❌ Errore lettura users.enc: %v", err)
		return err
	}
	log.Printf("📂 Letti %d byte da users.enc", len(encrypted))
	data, err := decryptData(encrypted)
	if err != nil {
		log.Printf("❌ Errore decifratura users.enc: %v", err)
		return err
	}
	var loaded map[string]*User
	if err := json.Unmarshal(data, &loaded); err != nil {
		log.Printf("❌ Errore unmarshal users: %v", err)
		return err
	}
	log.Printf("✅ Caricati %d utenti", len(loaded))
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
	return ioutil.WriteFile(filepath.Join(dataDir, "roles.enc"), encrypted, 0644)
}

func loadRoles(dataDir string) error {
	encrypted, err := ioutil.ReadFile(filepath.Join(dataDir, "roles.enc"))
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
	encrypted, err := ioutil.ReadFile(filepath.Join(dataDir, "remote_creds.enc"))
	if os.IsNotExist(err) {
		return nil, nil
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
	return ioutil.WriteFile(filepath.Join(dataDir, "remote_creds.enc"), encrypted, 0644)
}
