package main

import (
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// Lista utenti
func adminUsersPage(w http.ResponseWriter, r *http.Request) {
	userMutex.RLock()
	userList := make([]*User, 0, len(users))
	for _, u := range users {
		userList = append(userList, u)
	}
	userMutex.RUnlock()
	username, _ := getUserContext(r)
	perms := getUserPermissions(username)

	data := struct {
		Username        string
		IsAdmin         bool
		Title           string
		ContentTemplate string
		Users           []*User
		Permissions     map[string]bool
		Roles           []*Role
		IsMultiCPU      bool
	}{
		Username:        username,
		IsAdmin:         true,
		Title:           "Gestione Utenti",
		ContentTemplate: "adminUsersContent",
		Users:           userList,
		Permissions:     perms,
		Roles:           roles,
		IsMultiCPU:      isMultiCPU(),
	}
	tmpl.ExecuteTemplate(w, "layout.html", data)
}

// Crea utente
func adminUserCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/admin/users", http.StatusFound)
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	role := UserRole(r.FormValue("role"))
	if username == "" {
		http.Error(w, "Username richiesto", http.StatusBadRequest)
		return
	}
	userMutex.Lock()
	defer userMutex.Unlock()
	if _, exists := users[username]; exists {
		http.Error(w, "Utente già esistente", http.StatusConflict)
		return
	}
	defaultPwd := username + "123"
	hashDefault, _ := hashPassword(defaultPwd)
	now := time.Now()
	users[username] = &User{
		ID:                username,
		PasswordHash:      hashDefault,
		PasswordHistory:   []string{},
		Role:              role,
		MustChangePwd:     true,
		PasswordChangedAt: now,
		Enabled:           true,
		LastModified:      now,
	}
	saveUsers(currentDataDir)

	// 🔄 Sincronizza il file users.enc sulle macchine remote
	go func(userName string) {
		usersPath := filepath.Join(currentDataDir, "users.enc")
		if err := SyncFileToAllRemotes(usersPath); err != nil {
			log.Printf("❌ Errore sincronizzazione utenti (creazione): %v", err)
		} else {
			log.Printf("✅ Utenti sincronizzati dopo creazione di '%s'", userName)
		}
	}(username)

	http.Redirect(w, r, "/admin/users", http.StatusFound)
}

// Elimina utente
func adminUserDelete(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("username")
	userMutex.Lock()
	defer userMutex.Unlock()
	if _, exists := users[username]; !exists {
		http.NotFound(w, r)
		return
	}
	if username == "admin" {
		http.Error(w, "Non puoi eliminare l'admin principale", http.StatusForbidden)
		return
	}
	delete(users, username)
	saveUsers(currentDataDir)

	// 🔄 Sincronizza il file users.enc sulle macchine remote
	go func(userName string) {
		usersPath := filepath.Join(currentDataDir, "users.enc")
		if err := SyncFileToAllRemotes(usersPath); err != nil {
			log.Printf("❌ Errore sincronizzazione utenti (eliminazione): %v", err)
		} else {
			log.Printf("✅ Utenti sincronizzati dopo eliminazione di '%s'", userName)
		}
	}(username)

	http.Redirect(w, r, "/admin/users", http.StatusFound)
}

// Form modifica utente
func adminUserEditForm(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("username")
	userMutex.RLock()
	u := getUserByUsername(username)
	userMutex.RUnlock()
	if u == nil {
		http.NotFound(w, r)
		return
	}
	isProtected := false
	for _, pu := range config.ProtectedUsers {
		if pu == username {
			isProtected = true
			break
		}
	}
	usernameCtx, _ := getUserContext(r)
	perms := getUserPermissions(usernameCtx)
	rolesList := getAllRoles()

	data := struct {
		Username        string
		IsAdmin         bool
		Title           string
		ContentTemplate string
		User            *User
		IsProtected     bool
		Permissions     map[string]bool
		RolesList       []*Role
		IsMultiCPU      bool
	}{
		Username:        usernameCtx,
		IsAdmin:         true,
		Title:           "Modifica Utente",
		ContentTemplate: "adminUserEditContent",
		User:            u,
		IsProtected:     isProtected,
		Permissions:     perms,
		RolesList:       rolesList,
		IsMultiCPU:      isMultiCPU(),
	}
	tmpl.ExecuteTemplate(w, "layout.html", data)
}

// Salva modifiche utente (e reset password)
func adminUserEditPost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/admin/users", http.StatusFound)
		return
	}
	username := r.FormValue("username")
	action := r.FormValue("action")

	userMutex.Lock()
	defer userMutex.Unlock()
	u := getUserByUsername(username)
	if u == nil {
		http.NotFound(w, r)
		return
	}
	isProtected := false
	for _, pu := range config.ProtectedUsers {
		if pu == username {
			isProtected = true
			break
		}
	}
	if action == "delete" {
		if isProtected {
			http.Error(w, "Non puoi eliminare questo utente", http.StatusForbidden)
			return
		}
		delete(users, username)
		saveUsers(currentDataDir)

		// 🔄 Sincronizza il file users.enc sulle macchine remote
		go func(userName string) {
			usersPath := filepath.Join(currentDataDir, "users.enc")
			if err := SyncFileToAllRemotes(usersPath); err != nil {
				log.Printf("❌ Errore sincronizzazione utenti (eliminazione da edit): %v", err)
			} else {
				log.Printf("✅ Utenti sincronizzati dopo eliminazione di '%s'", userName)
			}
		}(username)

		http.Redirect(w, r, "/admin/users", http.StatusFound)
		return
	}
	if !isProtected {
		u.Role = UserRole(r.FormValue("role"))
		u.Enabled = r.FormValue("enabled") == "on"
		u.LastModified = time.Now()
	}
	if r.FormValue("reset_password") == "on" {
		defaultPwd := username + "123"
		hashDefault, err := hashPassword(defaultPwd)
		if err != nil {
			http.Error(w, "Errore interno", http.StatusInternalServerError)
			return
		}
		updatePasswordHistory(u, u.PasswordHash)
		u.PasswordHash = hashDefault
		u.MustChangePwd = true
		u.PasswordChangedAt = time.Now()
		u.LastModified = time.Now()
		saveUsers(currentDataDir)

		// 🔄 Sincronizza il file users.enc sulle macchine remote (reset password)
		go func(userName string) {
			usersPath := filepath.Join(currentDataDir, "users.enc")
			if err := SyncFileToAllRemotes(usersPath); err != nil {
				log.Printf("❌ Errore sincronizzazione utenti (reset password): %v", err)
			} else {
				log.Printf("✅ Utenti sincronizzati dopo reset password di '%s'", userName)
			}
		}(username)

		http.Redirect(w, r, "/admin/users", http.StatusFound)
		return
	}
	saveUsers(currentDataDir)

	// 🔄 Sincronizza il file users.enc sulle macchine remote (modifica)
	go func(userName string) {
		usersPath := filepath.Join(currentDataDir, "users.enc")
		if err := SyncFileToAllRemotes(usersPath); err != nil {
			log.Printf("❌ Errore sincronizzazione utenti (modifica): %v", err)
		} else {
			log.Printf("✅ Utenti sincronizzati dopo modifica di '%s'", userName)
		}
	}(username)

	http.Redirect(w, r, "/admin/users", http.StatusFound)
}

func adminUserUnlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	username := r.URL.Query().Get("username")
	userMutex.Lock()
	defer userMutex.Unlock()
	u := getUserByUsername(username)
	if u == nil {
		http.NotFound(w, r)
		return
	}
	// Sblocca l'account
	u.FailedLoginAttempts = 0
	u.LockedUntil = time.Time{}
	saveUsers(currentDataDir)

	log.Printf("Admin ha sbloccato l'account %s", username)
	WriteAuditLog("user_unlock", username, "Account sbloccato da admin")

	// 🔄 Sincronizza il file users.enc sulle macchine remote (sblocco)
	go func(userName string) {
		usersPath := filepath.Join(currentDataDir, "users.enc")
		if err := SyncFileToAllRemotes(usersPath); err != nil {
			log.Printf("❌ Errore sincronizzazione utenti (sblocco): %v", err)
		} else {
			log.Printf("✅ Utenti sincronizzati dopo sblocco di '%s'", userName)
		}
	}(username)

	w.WriteHeader(http.StatusOK)
}
