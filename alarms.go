package main

import (
	"encoding/json"
	"log"
	"net/http"
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
	}{
		Username:        username,
		IsAdmin:         isAdmin,
		Title:           "Allarmi",       // o "Alarms"
		ContentTemplate: "alarmsContent", // o "alarmsContent" se rinominato
		Permissions:     perms,
		Signals:         signals,
	}
	tmpl.ExecuteTemplate(w, "layout.html", data)
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
