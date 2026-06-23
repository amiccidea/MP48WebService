package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
)

func adminSettingsPage(w http.ResponseWriter, r *http.Request) {
	username, _ := getUserContext(r)
	perms := getUserPermissions(username)
	data := struct {
		Username        string
		IsAdmin         bool
		Title           string
		ContentTemplate string
		PasswordExpiry  int
		Permissions     map[string]bool
		IsMultiCPU      bool
	}{
		Username:        username,
		IsAdmin:         true,
		Title:           "Impostazioni",
		ContentTemplate: "adminSettingsContent",
		PasswordExpiry:  settings.PasswordExpiryDays,
		Permissions:     perms,
		IsMultiCPU:      isMultiCPU(),
	}
	tmpl.ExecuteTemplate(w, "layout.html", data)
}

func adminSettingsSave(w http.ResponseWriter, r *http.Request) {
	expiry, _ := strconv.Atoi(r.FormValue("password_expiry"))
	if expiry < 0 {
		expiry = 0
	}

	// Aggiorna la variabile globale
	settings.PasswordExpiryDays = expiry

	// Aggiorna la configurazione
	config.PasswordExpiryDays = expiry

	// Salva config.json
	configPath := "config.json"

	// Leggi il file attuale per preservare gli altri campi
	existingData, err := os.ReadFile(configPath)
	if err != nil {
		log.Printf("Errore lettura config.json per salvataggio: %v", err)
		http.Error(w, "Errore salvataggio", http.StatusInternalServerError)
		return
	}

	var configMap map[string]interface{}
	if err := json.Unmarshal(existingData, &configMap); err != nil {
		log.Printf("Errore parsing config.json: %v", err)
		http.Error(w, "Errore salvataggio", http.StatusInternalServerError)
		return
	}

	// Aggiorna il campo password_expiry_days
	configMap["password_expiry_days"] = expiry

	// Riscrivi il file
	newData, err := json.MarshalIndent(configMap, "", "    ")
	if err != nil {
		log.Printf("Errore marshalling config.json: %v", err)
		http.Error(w, "Errore salvataggio", http.StatusInternalServerError)
		return
	}

	if err := os.WriteFile(configPath, newData, 0644); err != nil {
		log.Printf("Errore scrittura config.json: %v", err)
		http.Error(w, "Errore salvataggio", http.StatusInternalServerError)
		return
	}

	// 🔄 Sincronizza config.json sulle macchine remote
	go func() {
		absPath, err := filepath.Abs(configPath)
		if err != nil {
			absPath = configPath
		}
		if err := SyncFileToAllRemotes(absPath); err != nil {
			log.Printf("❌ Errore sincronizzazione config.json: %v", err)
		} else {
			log.Printf("✅ config.json sincronizzato (password_expiry_days=%d)", expiry)
		}
	}()

	http.Redirect(w, r, "/admin/settings", http.StatusFound)
}
