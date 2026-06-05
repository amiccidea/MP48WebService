package main

import (
	"net/http"
)

//type Permission string

const (
	PermDashboard     string = "dashboard"
	PermAlarms        string = "alarms"
	PermLogs          string = "logs"
	PermMachineStatus string = "machine_status"
	PermConfigHistory string = "config_history"
	PermConfigUpload  string = "config_upload"
	PermAdminUsers    string = "admin_users"
	PermAdminRoles    string = "admin_roles"
	PermAdminSettings string = "admin_settings"
	PermAnalogInputs  string = "analog_inputs"
)

// hasPermission verifica se l'utente corrente ha il permesso richiesto
func hasPermission(r *http.Request, perm string) bool {
	username, _ := getUserContext(r)
	if username == "" {
		return false
	}
	perms := getUserPermissions(username)
	return perms[perm]
}

// permissionMiddleware restituisce un middleware che controlla il permesso
func permissionMiddleware(required string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !hasPermission(r, required) {
				http.Error(w, "Accesso negato: permesso insufficiente", http.StatusForbidden)
				return
			}
			next(w, r)
		}
	}
}
