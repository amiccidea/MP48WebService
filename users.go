package main

import (
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/csrf"
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
		CSRFField       template.HTML
		CSRFToken       string
		MFAEnabled      bool
	}{
		Username:        username,
		IsAdmin:         true,
		Title:           "Gestione Utenti",
		ContentTemplate: "adminUsersContent",
		Users:           userList,
		Permissions:     perms,
		Roles:           roles,
		IsMultiCPU:      isMultiCPU(),
		CSRFField:       csrf.TemplateField(r),
		CSRFToken:       csrf.Token(r),
		MFAEnabled:      config.MFAEnabled,
	}
	if err := tmpl.ExecuteTemplate(w, "layout.html", data); err != nil {
		Errorf("Errore rendering users: %v", err)
		http.Error(w, "Errore interno", http.StatusInternalServerError)
	}
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
		TOTPForceSetup:    config.MFAEnabled,
	}
	saveUsers(currentDataDir)

	go func(userName string) {
		usersPath := filepath.Join(currentDataDir, "users.enc")
		if err := SyncFileToAllRemotes(usersPath); err != nil {
			Errorf("Errore sincronizzazione utenti (creazione): %v", err)
		} else {
			Infof("Utenti sincronizzati dopo creazione di '%s'", userName)
		}
	}(username)

	WriteAuditLogWithRequest("USER_CREATE", username, fmt.Sprintf("Creato utente %s con ruolo %s", username, role), r)
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

	go func(userName string) {
		usersPath := filepath.Join(currentDataDir, "users.enc")
		if err := SyncFileToAllRemotes(usersPath); err != nil {
			Errorf("Errore sincronizzazione utenti (eliminazione): %v", err)
		} else {
			Infof("Utenti sincronizzati dopo eliminazione di '%s'", userName)
		}
	}(username)

	WriteAuditLogWithRequest("USER_DELETE", username, fmt.Sprintf("Eliminato utente %s", username), r)
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

	isLocked := !u.LockedUntil.IsZero() && time.Now().Before(u.LockedUntil)

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
		CSRFField       template.HTML
		CSRFToken       string
		MFAEnabled      bool
		TOTPEnabled     bool
		IsLocked        bool
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
		CSRFField:       csrf.TemplateField(r),
		CSRFToken:       csrf.Token(r),
		MFAEnabled:      config.MFAEnabled,
		TOTPEnabled:     u.TOTPEnabled,
		IsLocked:        isLocked,
	}
	if err := tmpl.ExecuteTemplate(w, "layout.html", data); err != nil {
		Errorf("Errore rendering user edit: %v", err)
		http.Error(w, "Errore interno", http.StatusInternalServerError)
	}
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

	// RESET MFA
	if action == "reset_mfa" {
		if !config.MFAEnabled {
			http.Error(w, "MFA non è abilitato globalmente", http.StatusBadRequest)
			return
		}
		if !u.TOTPEnabled {
			http.Error(w, "L'utente non ha MFA attivo", http.StatusBadRequest)
			return
		}
		u.TOTPEnabled = false
		u.TOTPSecret = ""
		u.TOTPBackupCodes = []string{}
		u.TOTPForceSetup = true
		u.LastModified = time.Now()
		saveUsers(currentDataDir)

		go func(userName string) {
			usersPath := filepath.Join(currentDataDir, "users.enc")
			if err := SyncFileToAllRemotes(usersPath); err != nil {
				Errorf("Errore sincronizzazione utenti (reset MFA per %s): %v", userName, err)
			} else {
				Infof("Utenti sincronizzati dopo reset MFA di '%s'", userName)
			}
		}(username)

		WriteAuditLogWithRequest("MFA_RESET_ADMIN", username, fmt.Sprintf("Reset MFA per %s", username), r)
		http.Redirect(w, r, "/admin/users", http.StatusFound)
		return
	}

	// ELIMINA UTENTE
	if action == "delete" {
		if isProtected {
			http.Error(w, "Non puoi eliminare questo utente", http.StatusForbidden)
			return
		}
		delete(users, username)
		saveUsers(currentDataDir)

		go func(userName string) {
			usersPath := filepath.Join(currentDataDir, "users.enc")
			if err := SyncFileToAllRemotes(usersPath); err != nil {
				Errorf("Errore sincronizzazione utenti (eliminazione da edit): %v", err)
			} else {
				Infof("Utenti sincronizzati dopo eliminazione di '%s'", userName)
			}
		}(username)

		WriteAuditLogWithRequest("USER_DELETE", username, fmt.Sprintf("Eliminato utente %s da edit", username), r)
		http.Redirect(w, r, "/admin/users", http.StatusFound)
		return
	}

	// SALVA MODIFICA
	if !isProtected {
		u.Role = UserRole(r.FormValue("role"))
		u.Enabled = r.FormValue("enabled") == "on"
		u.LastModified = time.Now()
	}

	// Reset password
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

		go func(userName string) {
			usersPath := filepath.Join(currentDataDir, "users.enc")
			if err := SyncFileToAllRemotes(usersPath); err != nil {
				Errorf("Errore sincronizzazione utenti (reset password): %v", err)
			} else {
				Infof("Utenti sincronizzati dopo reset password di '%s'", userName)
			}
		}(username)

		WriteAuditLogWithRequest("PASSWORD_RESET_ADMIN", username, fmt.Sprintf("Reset password per %s", username), r)
		http.Redirect(w, r, "/admin/users", http.StatusFound)
		return
	}

	saveUsers(currentDataDir)

	go func(userName string) {
		usersPath := filepath.Join(currentDataDir, "users.enc")
		if err := SyncFileToAllRemotes(usersPath); err != nil {
			Errorf("Errore sincronizzazione utenti (modifica): %v", err)
		} else {
			Infof("Utenti sincronizzati dopo modifica di '%s'", userName)
		}
	}(username)

	WriteAuditLogWithRequest("USER_EDIT", username, fmt.Sprintf("Modificato utente %s", username), r)
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
	u.FailedLoginAttempts = 0
	u.LockedUntil = time.Time{}
	saveUsers(currentDataDir)

	Infof("Admin ha sbloccato l'account %s", username)
	WriteAuditLogWithRequest("user_unlock", username, "Account sbloccato da admin", r)

	go func(userName string) {
		usersPath := filepath.Join(currentDataDir, "users.enc")
		if err := SyncFileToAllRemotes(usersPath); err != nil {
			Errorf("Errore sincronizzazione utenti (sblocco): %v", err)
		} else {
			Infof("Utenti sincronizzati dopo sblocco di '%s'", userName)
		}
	}(username)

	w.WriteHeader(http.StatusOK)
}