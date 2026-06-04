package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

func dashboardHandler(w http.ResponseWriter, r *http.Request) {
	username, isAdmin := getUserContext(r)
	if username == "" {
		http.Redirect(w, r, "/logout", http.StatusFound)
		return
	}
	perms := getUserPermissions(username)
	data := struct {
		Username        string
		IsAdmin         bool
		Title           string
		ContentTemplate string
		Permissions     map[string]bool
	}{
		Username:        username,
		IsAdmin:         isAdmin,
		Title:           "Dashboard",
		ContentTemplate: "dashboardContent",
		Permissions:     perms,
	}
	err := tmpl.ExecuteTemplate(w, "layout.html", data)
	if err != nil {
		http.Error(w, fmt.Sprintf("Errore nel template: %v", err), http.StatusInternalServerError)
		log.Printf("Errore ExecuteTemplate: %v", err)
		WriteAuditLog("dashboard_access", username, "Errore all'accesso alla dashboard:"+err.Error())
	}
}
func apiDashboardHandler(w http.ResponseWriter, r *http.Request) {
	session, err := store.Get(r, "portal-session")
	if err != nil || session.Values["authenticated"] != true {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	info := getSystemInfo()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}
