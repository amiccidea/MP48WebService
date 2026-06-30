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

	"github.com/gorilla/csrf"
	"github.com/gorilla/sessions"
	"golang.org/x/crypto/bcrypt"
)

func init() {
	initConfig()
	os.MkdirAll(config.ConfigHistoryDir, 0755)
	os.MkdirAll(config.UploadDir, 0755)

	// Determina se usare cookie Secure (solo se HTTPS è configurato)
	secureFlag := false
	if config.TLSCertFile != "" && config.TLSKeyFile != "" {
		secureFlag = true
	}

	store = sessions.NewCookieStore([]byte(config.SessionSecret))
	store.MaxAge(config.SessionMaxAgeSecond)
	store.Options = &sessions.Options{
		Path:     "/",
		HttpOnly: true,
		Secure:   secureFlag,
		SameSite: http.SameSiteLaxMode,
	}

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

	// Carica credenziali remote (dopo che currentDataDir è impostato)
	loadRemoteCredentialsFromDir()
}

// loadRemoteCredentialsFromDir carica le credenziali remote e le applica a config.RemoteMachines
func loadRemoteCredentialsFromDir() {
	remoteCreds, err := loadRemoteCredentials(currentDataDir)
	if err != nil {
		log.Printf("Errore caricamento credenziali remote: %v", err)
		return
	}
	if remoteCreds != nil && remoteCreds.Machines != nil {
		for i := range config.RemoteMachines {
			machineID := config.RemoteMachines[i].ID
			if cred, ok := remoteCreds.Machines[machineID]; ok {
				config.RemoteMachines[i].FTP.Username = cred.FTPUsername
				config.RemoteMachines[i].FTP.Password = cred.FTPPassword
				config.RemoteMachines[i].Telnet.Username = cred.TelnetUsername
				config.RemoteMachines[i].Telnet.Password = cred.TelnetPassword
				config.RemoteMachines[i].Telnet.SudoPassword = cred.SudoPassword
			}
		}
		log.Printf("Credenziali remote caricate per %d macchine", len(remoteCreds.Machines))
	} else {
		log.Println("AVVISO: Nessuna credenziale remota configurata. Usare l'interfaccia admin per impostarle.")
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

// In una funzione helper, o in getLayoutData:
func getCSRFField(r *http.Request) template.HTML {
	return csrf.TemplateField(r)
}

func getLayoutData(r *http.Request, title, contentTemplate string) map[string]interface{} {
	username, isAdmin := getUserContext(r)
	perms := getUserPermissions(username)
	multiCPU := isMultiCPU()
	token := csrf.Token(r)
	log.Printf("DEBUG: Username=%s, IsAdmin=%v, IsMultiCPU=%v", username, isAdmin, multiCPU)
	return map[string]interface{}{
		"Username":        username,
		"IsAdmin":         isAdmin,
		"Title":           title,
		"ContentTemplate": contentTemplate,
		"Permissions":     perms,
		"IsMultiCPU":      multiCPU,
		"CSRFToken":       token,
		"CSRFField":       csrf.TemplateField(r),
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

	ok := false
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
		u.FailedLoginAttempts = 0
		u.LockedUntil = time.Time{}
		saveUsers(currentDataDir)
		WriteAuditLog("login_success", username, "Login riuscito")
		log.Printf("Login riuscito per %s", username)
		return true
	}

	u.FailedLoginAttempts++
	WriteAuditLog("login_failed", username, "Tentativo di accesso fallito")
	log.Printf("Tentativo di accesso fallito per %s (%d/5)", username, u.FailedLoginAttempts)

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

// updateSessionActivity aggiorna il timestamp di ultima attività nella sessione.
func updateSessionActivity(session *sessions.Session) {
	session.Values["last_activity"] = time.Now().Unix()
}

// isSessionExpired controlla se la sessione è scaduta per inattività.
func isSessionExpired(session *sessions.Session) bool {
	lastActivityVal, ok := session.Values["last_activity"]
	if !ok {
		return false
	}
	lastActivity, ok := lastActivityVal.(int64)
	if !ok {
		return false
	}
	lastTime := time.Unix(lastActivity, 0)
	inactivityMinutes := config.SessionInactivityMinutes
	if inactivityMinutes <= 0 {
		inactivityMinutes = 30
	}
	if time.Since(lastTime) > time.Duration(inactivityMinutes)*time.Minute {
		return true
	}
	return false
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

func isSessionAbsoluteExpired(session *sessions.Session) bool {
	createdAtVal, ok := session.Values["created_at"]
	if !ok {
		return true // se non c'è, consideriamo scaduto per sicurezza
	}
	createdAt, ok := createdAtVal.(int64)
	if !ok {
		return true
	}
	absoluteHours := config.SessionAbsoluteHours
	if absoluteHours <= 0 {
		absoluteHours = 4 // default 4 ore
	}
	createdTime := time.Unix(createdAt, 0)
	if time.Since(createdTime) > time.Duration(absoluteHours)*time.Hour {
		return true
	}
	return false
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
		if isSessionExpired(session) {
			session.Values["authenticated"] = false
			session.Options.MaxAge = -1
			session.Save(r, w)
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		// Controllo timeout assoluto
		if isSessionAbsoluteExpired(session) {
			session.Values["authenticated"] = false
			session.Options.MaxAge = -1
			session.Save(r, w)
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		updateSessionActivity(session)
		session.Save(r, w)
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
		data := map[string]interface{}{
			"CSRFField": csrf.TemplateField(r),
			"CSRFToken": csrf.Token(r),
		}
		tmpl.ExecuteTemplate(w, "login.html", data)
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
		now := time.Now().Unix()
		session.Values["last_activity"] = now
		session.Values["created_at"] = now
		session.Save(r, w)
		log.Printf("Login riuscito per %s (admin=%v)", username, u.Role == RoleAdmin)
		WriteAuditLog("login_success", username, "Login riuscito")
		http.Redirect(w, r, "/alarms", http.StatusFound)
		return
	}

	u := getUserByUsername(username)
	if u != nil && !u.LockedUntil.IsZero() && time.Now().Before(u.LockedUntil) {
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
		"CSRFField":           csrf.TemplateField(r),
		"CSRFToken":           csrf.Token(r),
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

	// 🔄 Sincronizza il file users.enc sulle macchine remote (cambio password forzato)
	go func(userName string) {
		usersPath := filepath.Join(currentDataDir, "users.enc")
		if err := SyncFileToAllRemotes(usersPath); err != nil {
			log.Printf("❌ Errore sincronizzazione utenti (cambio password forzato per %s): %v", userName, err)
		} else {
			log.Printf("✅ Utenti sincronizzati dopo cambio password forzato di '%s'", userName)
		}
	}(username)

	delete(session.Values, "pending_user")
	session.Values["authenticated"] = true
	session.Values["username"] = username
	session.Values["is_admin"] = (u.Role == RoleAdmin)
	session.Values["last_activity"] = time.Now().Unix()
	session.Save(r, w)
	http.Redirect(w, r, "/alarms", http.StatusFound)
}

func profileChangePasswordPage(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Forced":              false,
		"OldPasswordRequired": true,
		"CSRFField":           csrf.TemplateField(r),
		"CSRFToken":           csrf.Token(r),
	}
	tmpl.ExecuteTemplate(w, "change_password.html", data)
}

func getUsernameFromContext(r *http.Request) string {
	username, _ := getUserContext(r)
	return username
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

	// 🔄 Sincronizza il file users.enc sulle macchine remote (cambio password volontario)
	go func(userName string) {
		usersPath := filepath.Join(currentDataDir, "users.enc")
		if err := SyncFileToAllRemotes(usersPath); err != nil {
			log.Printf("❌ Errore sincronizzazione utenti (cambio password volontario per %s): %v", userName, err)
		} else {
			log.Printf("✅ Utenti sincronizzati dopo cambio password volontario di '%s'", userName)
		}
	}(username)

	session, _ := store.Get(r, "portal-session")
	session.Values["authenticated"] = true
	session.Values["username"] = username
	session.Values["is_admin"] = isAdmin
	session.Values["last_activity"] = time.Now().Unix()
	session.Save(r, w)
	http.Redirect(w, r, "/alarms", http.StatusFound)
}
