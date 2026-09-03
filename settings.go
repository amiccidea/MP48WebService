package main

import (
	"encoding/json"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/gorilla/csrf"
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
		CSRFField       template.HTML
		CSRFToken       string
		MFAEnabled		bool
	}{
		Username:        username,
		IsAdmin:         true,
		Title:           "Impostazioni",
		ContentTemplate: "adminSettingsContent",
		PasswordExpiry:  settings.PasswordExpiryDays,
		Permissions:     perms,
		IsMultiCPU:      isMultiCPU(),
		CSRFField:       csrf.TemplateField(r),
		CSRFToken:       csrf.Token(r),
		MFAEnabled:      config.MFAEnabled,
	}
	tmpl.ExecuteTemplate(w, "layout.html", data)
}

func adminSettingsSave(w http.ResponseWriter, r *http.Request) {
	expiry, _ := strconv.Atoi(r.FormValue("password_expiry"))
	if expiry < 0 {
		expiry = 0
	}

	settings.PasswordExpiryDays = expiry
	config.PasswordExpiryDays = expiry

	configPath := "config.json"
	existingData, err := ioutil.ReadFile(configPath)
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

	configMap["password_expiry_days"] = expiry

	newData, err := json.MarshalIndent(configMap, "", "    ")
	if err != nil {
		log.Printf("Errore marshalling config.json: %v", err)
		http.Error(w, "Errore salvataggio", http.StatusInternalServerError)
		return
	}

	if err := ioutil.WriteFile(configPath, newData, 0644); err != nil {
		log.Printf("Errore scrittura config.json: %v", err)
		http.Error(w, "Errore salvataggio", http.StatusInternalServerError)
		return
	}

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
