package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/sessions"
	"golang.org/x/crypto/bcrypt"
)

func init() {
	initConfig()
	os.MkdirAll(config.ConfigHistoryDir, 0755)
	os.MkdirAll(config.UploadDir, 0755)

	store = sessions.NewCookieStore([]byte(config.SessionSecret))
	store.MaxAge(config.SessionMaxAgeSecond)
	store.Options = &sessions.Options{
		Path:     "/",
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
	}

	// Template parsing
	funcMap := template.FuncMap{
		"getAllPermissions": getAllPermissions,
		"permissionLabel":   permissionLabel,
		"roleName":          getRoleName,
		"toJSON": func(v interface{}) string {
			b, _ := json.Marshal(v)
			return string(b)
		},
	}
	tmpl = template.Must(template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/*.html"))

	// Inizializza persistenza
	currentDataDir = config.DataDir
	if currentDataDir == "" {
		currentDataDir = "./data"
	}
	os.MkdirAll(currentDataDir, 0700)
	keyPath := filepath.Join(currentDataDir, "encryption.key")
	var err error
	encryptionKey, err = loadOrGenerateKey(keyPath)
	if err != nil {
		log.Fatal("Errore chiave crittografia:", err)
	}
	if err := loadUsers(currentDataDir); err != nil {
		log.Printf("Errore caricamento utenti, inizializzo default: %v", err)
		userMutex.Lock()
		initDefaultUsers()
		saveUsers(currentDataDir)
		userMutex.Unlock()
	}
	if err := loadRoles(currentDataDir); err != nil {
		log.Printf("Errore caricamento ruoli, uso default: %v", err)
		saveRoles(currentDataDir)
	}
}

func initDefaultUsers() {
	if len(users) > 0 {
		return
	}
	now := time.Now()
	hashAdmin, _ := hashPassword("admin123")
	hashOperatore, _ := hashPassword("operatore123")
	hashGuest, _ := hashPassword("guest123")
	users["admin"] = &User{
		ID:                "admin",
		PasswordHash:      hashAdmin,
		PasswordHistory:   []string{},
		Role:              RoleAdmin,
		MustChangePwd:     true,
		PasswordChangedAt: now,
		Enabled:           true,
		LastModified:      now,
	}
	users["operatore"] = &User{
		ID:                "operatore",
		PasswordHash:      hashOperatore,
		PasswordHistory:   []string{},
		Role:              "tech",
		MustChangePwd:     true,
		PasswordChangedAt: now,
		Enabled:           true,
		LastModified:      now,
	}
	users["guest"] = &User{
		ID:                "guest",
		PasswordHash:      hashGuest,
		PasswordHistory:   []string{},
		Role:              "user",
		MustChangePwd:     true,
		PasswordChangedAt: now,
		Enabled:           true,
		LastModified:      now,
	}
}

func getLayoutData(r *http.Request, title, contentTemplate string) map[string]interface{} {
	username, isAdmin := getUserContext(r)
	perms := getUserPermissions(username)
	return map[string]interface{}{
		"Username":        username,
		"IsAdmin":         isAdmin,
		"Title":           title,
		"ContentTemplate": contentTemplate,
		"Permissions":     perms,
	}
}

func authenticateLocal(username, password string) bool {
	userMutex.Lock()
	defer userMutex.Unlock()

	u := getUserByUsername(username)
	if u == nil {
		log.Printf("Tentativo di accesso per utente inesistente: %s", username)
		WriteAuditLog("login_failed", username, "Tentativo di accesso per utente inesistente")
		return false
	}

	// Controlla se l'account è bloccato
	if !u.LockedUntil.IsZero() && time.Now().Before(u.LockedUntil) {
		log.Printf("Accesso negato per utente bloccato: %s (bloccato fino a %s)", username, u.LockedUntil.Format(time.RFC3339))
		WriteAuditLog("login_failed", username, "Accesso negato per utente bloccato")
		return false
	}

	if !u.Enabled {
		log.Printf("Tentativo di accesso per utente disabilitato: %s", username)
		WriteAuditLog("login_failed", username, "Tentativo di accesso per utente disabilitato")
		return false
	}

	// Verifica password
	ok := false
	// Migrazione plain text
	if !strings.HasPrefix(u.PasswordHash, "$2a$") && !strings.HasPrefix(u.PasswordHash, "$2b$") {
		if u.PasswordHash == password {
			newHash, err := hashPassword(password)
			if err == nil {
				u.PasswordHash = newHash
				u.PasswordHistory = []string{}
				u.LastModified = time.Now()
				ok = true
				log.Printf("Migrata password di %s a bcrypt", username)
			}
		}
	} else {
		err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password))
		ok = err == nil
	}

	if ok {
		// Reset tentativi falliti e sblocca se era bloccato
		u.FailedLoginAttempts = 0
		u.LockedUntil = time.Time{}
		saveUsers(currentDataDir)
		WriteAuditLog("login_success", username, "Login riuscito")
		log.Printf("Login riuscito per %s", username)
		return true
	}

	// Tentativo fallito: incrementa contatore
	u.FailedLoginAttempts++
	WriteAuditLog("login_failed", username, "Tentativo di accesso fallito")
	log.Printf("Tentativo di accesso fallito per %s (%d/5)", username, u.FailedLoginAttempts)

	// Dopo 5 tentativi, blocca l'account per 15 minuti
	if u.FailedLoginAttempts >= 5 {
		u.LockedUntil = time.Now().Add(15 * time.Minute)
		WriteAuditLog("login_failed", username, "Account bloccato per 15 minuti")
		log.Printf("Account %s bloccato per 15 minuti (fino a %s)", username, u.LockedUntil.Format(time.RFC3339))
	}
	saveUsers(currentDataDir)
	return false
}

func getUserByUsername(username string) *User {
	u, ok := users[username]
	if !ok {
		return nil
	}
	return u
}

func isPasswordExpired(u *User) bool {
	if settings.PasswordExpiryDays <= 0 {
		return false
	}
	return time.Since(u.PasswordChangedAt) > time.Duration(settings.PasswordExpiryDays)*24*time.Hour
}

func getUserContext(r *http.Request) (username string, isAdmin bool) {
	session, err := store.Get(r, "portal-session")
	if err != nil {
		return "", false
	}
	auth, ok := session.Values["authenticated"].(bool)
	if !ok || !auth {
		return "", false
	}
	usernameRaw := session.Values["username"]
	if usernameRaw == nil {
		return "", false
	}
	username, ok = usernameRaw.(string)
	if !ok {
		return "", false
	}
	adminRaw := session.Values["is_admin"]
	if adminRaw != nil {
		isAdmin, _ = adminRaw.(bool)
	}
	return
}

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, err := store.Get(r, "portal-session")
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		auth, ok := session.Values["authenticated"].(bool)
		if !ok || !auth {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		next(w, r)
	}
}

func adminMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, isAdmin := getUserContext(r)
		if !isAdmin {
			http.Error(w, "Accesso negato: area riservata agli amministratori", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// Login handler
func loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		tmpl.ExecuteTemplate(w, "login.html", nil)
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")

	if authenticateLocal(username, password) {
		u := getUserByUsername(username)
		if u == nil {
			tmpl.ExecuteTemplate(w, "login.html", map[string]string{"error": "Credenziali non valide"})
			return
		}
		if u.MustChangePwd || isPasswordExpired(u) {
			session, _ := store.Get(r, "portal-session")
			session.Values["pending_user"] = username
			session.Save(r, w)
			http.Redirect(w, r, "/change-password", http.StatusFound)
			return
		}
		session, _ := store.Get(r, "portal-session")
		session.Values["authenticated"] = true
		session.Values["username"] = username
		session.Values["is_admin"] = (u.Role == RoleAdmin)
		session.Save(r, w)
		log.Printf("Login riuscito per %s (admin=%v)", username, u.Role == RoleAdmin)
		WriteAuditLog("login_success", username, "Login riuscito")
		http.Redirect(w, r, "/dashboard", http.StatusFound)
		return
	}

	// Se l'autenticazione fallisce, verifica se l'utente esiste ed è bloccato
	u := getUserByUsername(username)
	if u != nil && !u.LockedUntil.IsZero() && time.Now().Before(u.LockedUntil) {
		// Mostra messaggio di blocco con l'orario di sblocco
		tmpl.ExecuteTemplate(w, "login.html", map[string]string{
			"error": fmt.Sprintf("Account bloccato fino alle %s", u.LockedUntil.Format("15:04:05")),
		})
	} else {
		tmpl.ExecuteTemplate(w, "login.html", map[string]string{"error": "Credenziali non valide"})
	}
}

// Change password handlers
func changePasswordPage(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "portal-session")
	pending := session.Values["pending_user"]
	if pending == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	data := map[string]interface{}{
		"Forced":              true,
		"OldPasswordRequired": false,
	}
	tmpl.ExecuteTemplate(w, "change_password.html", data)
}

func changePasswordPost(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "portal-session")
	pending := session.Values["pending_user"]
	if pending == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	username := pending.(string)
	newPwd := r.FormValue("new_password")
	confirm := r.FormValue("confirm_password")

	if err := validatePasswordComplexity(newPwd); err != nil {
		data := map[string]interface{}{
			"Forced":              true,
			"OldPasswordRequired": false,
			"Error":               err.Error(),
		}
		tmpl.ExecuteTemplate(w, "change_password.html", data)
		return
	}
	if newPwd != confirm {
		data := map[string]interface{}{
			"Forced":              true,
			"OldPasswordRequired": false,
			"Error":               "Le password non corrispondono",
		}
		tmpl.ExecuteTemplate(w, "change_password.html", data)
		return
	}

	userMutex.Lock()
	u := getUserByUsername(username)
	if u == nil {
		userMutex.Unlock()
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	if isPasswordReused(u, newPwd) {
		userMutex.Unlock()
		data := map[string]interface{}{
			"Forced":              true,
			"OldPasswordRequired": false,
			"Error":               "Password già utilizzata in precedenza. Scegline un'altra.",
		}
		tmpl.ExecuteTemplate(w, "change_password.html", data)
		return
	}
	oldHash := u.PasswordHash
	updatePasswordHistory(u, oldHash)
	newHash, err := hashPassword(newPwd)
	if err != nil {
		userMutex.Unlock()
		data := map[string]interface{}{
			"Forced":              true,
			"OldPasswordRequired": false,
			"Error":               "Errore interno durante l'elaborazione della password",
		}
		tmpl.ExecuteTemplate(w, "change_password.html", data)
		return
	}
	u.PasswordHash = newHash
	u.MustChangePwd = false
	u.PasswordChangedAt = time.Now()
	u.LastModified = time.Now()
	userMutex.Unlock()
	saveUsers(currentDataDir)

	delete(session.Values, "pending_user")
	session.Values["authenticated"] = true
	session.Values["username"] = username
	session.Values["is_admin"] = (u.Role == RoleAdmin)
	session.Save(r, w)
	http.Redirect(w, r, "/dashboard", http.StatusFound)
}

func profileChangePasswordPage(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Forced":              false,
		"OldPasswordRequired": true,
	}
	tmpl.ExecuteTemplate(w, "change_password.html", data)
}

func profileChangePasswordPost(w http.ResponseWriter, r *http.Request) {
	username, isAdmin := getUserContext(r)
	oldPwd := r.FormValue("old_password")
	newPwd := r.FormValue("new_password")
	confirm := r.FormValue("confirm_password")

	userMutex.Lock()
	defer userMutex.Unlock()
	u := getUserByUsername(username)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(oldPwd)); err != nil {
		data := map[string]interface{}{
			"Forced":              false,
			"OldPasswordRequired": true,
			"Error":               "Vecchia password errata",
		}
		tmpl.ExecuteTemplate(w, "change_password.html", data)
		return
	}
	if err := validatePasswordComplexity(newPwd); err != nil {
		data := map[string]interface{}{
			"Forced":              false,
			"OldPasswordRequired": true,
			"Error":               err.Error(),
		}
		tmpl.ExecuteTemplate(w, "change_password.html", data)
		return
	}
	if newPwd != confirm {
		data := map[string]interface{}{
			"Forced":              false,
			"OldPasswordRequired": true,
			"Error":               "Le nuove password non corrispondono",
		}
		tmpl.ExecuteTemplate(w, "change_password.html", data)
		return
	}
	if isPasswordReused(u, newPwd) {
		data := map[string]interface{}{
			"Forced":              false,
			"OldPasswordRequired": true,
			"Error":               "Password già utilizzata in precedenza",
		}
		tmpl.ExecuteTemplate(w, "change_password.html", data)
		return
	}
	oldHash := u.PasswordHash
	updatePasswordHistory(u, oldHash)
	newHash, err := hashPassword(newPwd)
	if err != nil {
		data := map[string]interface{}{
			"Forced":              false,
			"OldPasswordRequired": true,
			"Error":               "Errore interno",
		}
		tmpl.ExecuteTemplate(w, "change_password.html", data)
		return
	}
	u.PasswordHash = newHash
	u.MustChangePwd = false
	u.PasswordChangedAt = time.Now()
	u.LastModified = time.Now()
	saveUsers(currentDataDir)

	session, _ := store.Get(r, "portal-session")
	session.Values["authenticated"] = true
	session.Values["username"] = username
	session.Values["is_admin"] = isAdmin
	session.Save(r, w)
	http.Redirect(w, r, "/dashboard", http.StatusFound)
}
