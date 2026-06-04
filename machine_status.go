package main

import (
	"net/http"
)

func machineStatusHandler(w http.ResponseWriter, r *http.Request) {
	username, isAdmin := getUserContext(r)
	perms := getUserPermissions(username)
	data := struct {
		Username        string
		IsAdmin         bool
		Title           string
		ContentTemplate string
		Status          string
		Permissions     map[string]bool
	}{
		Username:        username,
		IsAdmin:         isAdmin,
		Title:           "Stato Macchina",
		ContentTemplate: "machineStatusContent",
		Status:          "Macchina operativa, mirror sincronizzato",
		Permissions:     perms,
	}
	tmpl.ExecuteTemplate(w, "layout.html", data)
}
