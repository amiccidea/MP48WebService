package main

import (
	"encoding/json"
	"html/template"
	"log"
	"net/http"

	"github.com/gorilla/csrf"
)

func alarmsHandler(w http.ResponseWriter, r *http.Request) {
	username, isAdmin := getUserContext(r)
	if username == "" {
		http.Redirect(w, r, "/logout", http.StatusFound)
		return
	}
	perms := getUserPermissions(username)
	signals, err := GetSignalsData()
	if err != nil {
		log.Printf("Errore recupero segnali: %v", err)
		signals = &SignalsData{}
	}

	data := struct {
		Username        string
		IsAdmin         bool
		Title           string
		ContentTemplate string
		Permissions     map[string]bool
		Signals         *SignalsData
		IsMultiCPU      bool
		CSRFField       template.HTML
		CSRFToken       string
	}{
		Username:        username,
		IsAdmin:         isAdmin, // ✅ usato!
		Title:           "Allarmi",
		ContentTemplate: "alarmsContent",
		Permissions:     perms,
		Signals:         signals,
		IsMultiCPU:      isMultiCPU(),
		CSRFField:       csrf.TemplateField(r),
		CSRFToken:       csrf.Token(r), // ✅ aggiunto
	}

	if err := tmpl.ExecuteTemplate(w, "layout.html", data); err != nil {
		log.Printf("❌ Errore rendering alarms: %v", err)
		http.Error(w, "Errore interno", http.StatusInternalServerError)
	}
}

func apiAlarmsHandler(w http.ResponseWriter, r *http.Request) {
	session, err := store.Get(r, "portal-session")
	if err != nil || session.Values["authenticated"] != true {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	info := getSystemInfo()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}
