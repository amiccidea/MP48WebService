package main

import (
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"strings"
)

var roles = []*Role{
	{
		ID:   "user",
		Name: "Utente",
		Permissions: map[string]bool{
			"alarms":         true, // ex dashboard
			"logs":           true,
			"machine_status": true,
			// analog_inputs: false (non lo vede)
		},
	},
	{
		ID:   "tech",
		Name: "Operatore Tecnico",
		Permissions: map[string]bool{
			"alarms":         true,
			"logs":           true,
			"machine_status": true,
			"config_history": true,
			"config_upload":  true,
			"analog_inputs":  true, // nuovo
		},
	},
	{
		ID:   "admin",
		Name: "Amministratore",
		Permissions: map[string]bool{
			"alarms":         true,
			"logs":           true,
			"machine_status": true,
			"config_history": true,
			"config_upload":  true,
			"admin_users":    true,
			"admin_roles":    true,
			"admin_settings": true,
			"analog_inputs":  true, // nuovo
		},
	},
}

func getAllPermissions() []string {
	return []string{
		"dashboard", "logs", "machine_status", "config_history",
		"config_upload", "admin_users", "admin_roles", "admin_settings",
	}
}

func permissionLabel(p string) string {
	labels := map[string]string{
		"dashboard": "Dashboard", "logs": "Scarica log",
		"machine_status": "Stato macchina e mirror",
		"config_history": "Storico configurazioni",
		"config_upload":  "Carica impostazioni",
		"admin_users":    "Gestione utenti",
		"admin_roles":    "Gestione ruoli",
		"admin_settings": "Impostazioni sistema",
	}
	return labels[p]
}

func rolesToJSON() string {
	rolesMutex.RLock()
	defer rolesMutex.RUnlock()
	b, _ := json.Marshal(roles)
	return string(b)
}

func adminRolesPage(w http.ResponseWriter, r *http.Request) {
	username, isAdmin := getUserContext(r)
	perms := getUserPermissions(username)
	data := struct {
		Username        string
		IsAdmin         bool
		Title           string
		ContentTemplate string
		RolesJSON       template.JS
		Permissions     map[string]bool
	}{
		Username:        username,
		IsAdmin:         isAdmin,
		Title:           "Gestione Ruoli",
		ContentTemplate: "adminRolesContent",
		RolesJSON:       template.JS(rolesToJSON()),
		Permissions:     perms,
	}
	tmpl.ExecuteTemplate(w, "layout.html", data)
}

func adminRolesCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/admin/roles", http.StatusFound)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "Nome ruolo richiesto", http.StatusBadRequest)
		return
	}
	id := strings.ToLower(strings.ReplaceAll(name, " ", "_"))
	rolesMutex.Lock()
	defer rolesMutex.Unlock()
	for _, rl := range roles {
		if rl.ID == id {
			http.Error(w, "Ruolo già esistente", http.StatusConflict)
			return
		}
	}
	newRole := &Role{
		ID:          id,
		Name:        name,
		Permissions: make(map[string]bool),
	}
	for _, p := range getAllPermissions() {
		newRole.Permissions[p] = false
	}
	roles = append(roles, newRole)
	saveRoles(currentDataDir)
	http.Redirect(w, r, "/admin/roles", http.StatusFound)
}

func adminRolesDelete(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	rolesMutex.Lock()
	defer rolesMutex.Unlock()
	if id == "admin" || id == "user" || id == "tech" {
		http.Error(w, "Non puoi eliminare i ruoli predefiniti", http.StatusForbidden)
		return
	}
	for i, rl := range roles {
		if rl.ID == id {
			roles = append(roles[:i], roles[i+1:]...)
			saveRoles(currentDataDir)
			break
		}
	}
	http.Redirect(w, r, "/admin/roles", http.StatusFound)
}

func adminRolesUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/admin/roles", http.StatusFound)
		return
	}
	id := r.FormValue("id")
	rolesMutex.Lock()
	defer rolesMutex.Unlock()
	var targetRole *Role
	for _, rl := range roles {
		if rl.ID == id {
			targetRole = rl
			break
		}
	}
	if targetRole == nil {
		if id == "" {
			name := r.FormValue("name")
			if name == "" {
				http.Error(w, "Nome ruolo richiesto", http.StatusBadRequest)
				return
			}
			newId := strings.ToLower(strings.ReplaceAll(name, " ", "_"))
			for _, rl := range roles {
				if rl.ID == newId {
					http.Error(w, "Ruolo già esistente", http.StatusConflict)
					return
				}
			}
			newRole := &Role{
				ID:          newId,
				Name:        name,
				Permissions: make(map[string]bool),
			}
			for _, p := range getAllPermissions() {
				newRole.Permissions[p] = false
			}
			roles = append(roles, newRole)
			saveRoles(currentDataDir)
			http.Redirect(w, r, "/admin/roles", http.StatusFound)
			return
		}
		http.NotFound(w, r)
		return
	}
	name := r.FormValue("name")
	if name != "" {
		targetRole.Name = name
	}
	for _, p := range getAllPermissions() {
		val := r.FormValue(string(p)) == "on"
		targetRole.Permissions[p] = val
	}
	saveRoles(currentDataDir)
	http.Redirect(w, r, "/admin/roles", http.StatusFound)
}

// In roles.go, cambia la definizione di Role (opzionale) e getUserPermissions
func getUserPermissions(username string) map[string]bool {
	u := getUserByUsername(username)
	if u == nil {
		log.Printf("getUserPermissions: utente %s non trovato", username)
		return map[string]bool{}
	}
	for _, r := range roles {
		if r.ID == string(u.Role) {
			//log.Printf("Permessi per %s (ruolo %s): %v", username, r.ID, r.Permissions)
			return r.Permissions
		}
	}
	log.Printf("Ruolo non trovato per %s", username)
	return map[string]bool{}
}

func getRoleName(roleID string) string {
	for _, r := range roles {
		if r.ID == roleID {
			return r.Name
		}
	}
	return roleID // fallback
}

// getAllRoles restituisce la lista completa dei ruoli
func getAllRoles() []*Role {
	rolesMutex.RLock()
	defer rolesMutex.RUnlock()
	return roles
}
