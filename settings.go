package main

import (
	"net/http"
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
	}{
		Username:        username,
		IsAdmin:         true,
		Title:           "Impostazioni",
		ContentTemplate: "adminSettingsContent",
		PasswordExpiry:  settings.PasswordExpiryDays,
		Permissions:     perms,
	}
	tmpl.ExecuteTemplate(w, "layout.html", data)
}

func adminSettingsSave(w http.ResponseWriter, r *http.Request) {
	expiry, _ := strconv.Atoi(r.FormValue("password_expiry"))
	if expiry < 0 {
		expiry = 0
	}
	settings.PasswordExpiryDays = expiry
	http.Redirect(w, r, "/admin/settings", http.StatusFound)
}
