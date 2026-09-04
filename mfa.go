package main

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gorilla/csrf"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"github.com/skip2/go-qrcode"
	"golang.org/x/crypto/bcrypt"
)

// GenerateTOTPSecret genera un nuovo secret TOTP per l'utente
func GenerateTOTPSecret(username string) (string, string, error) {
	issuer := config.MFAIssuer
	if issuer == "" {
		issuer = "MP48WebService"
	}

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: username,
		Period:      30,
		Digits:      6,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		return "", "", err
	}
	return key.Secret(), key.URL(), nil
}

// VerifyTOTP verifica il codice TOTP inserito dall'utente
func VerifyTOTP(secret, code string) bool {
	return totp.Validate(code, secret)
}

// GenerateTOTPQRCode genera un'immagine PNG del QR code
func GenerateTOTPQRCode(url string) ([]byte, error) {
	return qrcode.Encode(url, qrcode.Medium, 256)
}

// generateBackupCodes genera codici di backup (8 caratteri alfanumerici)
func generateBackupCodes(count int) []string {
	codes := make([]string, count)
	for i := 0; i < count; i++ {
		b := make([]byte, 8)
		rand.Read(b)
		codes[i] = base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
	}
	return codes
}

// ===== HANDLER PAGINA MFA (PROFILO) =====
func mfaPageHandler(w http.ResponseWriter, r *http.Request) {
	if !config.MFAEnabled {
		http.NotFound(w, r)
		return
	}
	username, isAdmin := getUserContext(r)
	if username == "" {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	u := getUserByUsername(username)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	perms := getUserPermissions(username)
	data := struct {
		Username        string
		IsAdmin         bool
		Title           string
		ContentTemplate string
		Permissions     map[string]bool
		IsMultiCPU      bool
		CSRFField       template.HTML
		CSRFToken       string
		TOTPEnabled     bool
		TOTPSecret      bool
		MFAEnabled      bool
	}{
		Username:        username,
		IsAdmin:         isAdmin,
		Title:           "MFA - Autenticazione a due fattori",
		ContentTemplate: "mfaContent",
		Permissions:     perms,
		IsMultiCPU:      isMultiCPU(),
		CSRFField:       csrf.TemplateField(r),
		CSRFToken:       csrf.Token(r),
		TOTPEnabled:     u.TOTPEnabled,
		TOTPSecret:      u.TOTPSecret != "",
		MFAEnabled:      config.MFAEnabled,
	}
	if err := tmpl.ExecuteTemplate(w, "layout.html", data); err != nil {
		log.Printf("❌ Errore rendering pagina MFA: %v", err)
		http.Error(w, "Errore interno", http.StatusInternalServerError)
	}
}

// ===== HANDLER ATTIVAZIONE MFA =====
func mfaEnableHandler(w http.ResponseWriter, r *http.Request) {
	if !config.MFAEnabled {
		http.NotFound(w, r)
		return
	}
	username, isAdmin := getUserContext(r)
	u := getUserByUsername(username)
	if u == nil {
		http.Error(w, "Utente non trovato", http.StatusNotFound)
		return
	}
	if u.TOTPEnabled {
		http.Redirect(w, r, "/profile/mfa", http.StatusFound)
		return
	}

	action := r.FormValue("action")
	perms := getUserPermissions(username)

	if action == "generate" {
		secret, url, err := GenerateTOTPSecret(username)
		if err != nil {
			http.Error(w, "Errore generazione secret", http.StatusInternalServerError)
			return
		}
		session, _ := store.Get(r, "portal-session")
		session.Values["totp_secret_temp"] = secret
		session.Values["totp_url"] = url
		session.Save(r, w)

		qrBytes, err := GenerateTOTPQRCode(url)
		if err != nil {
			http.Error(w, "Errore generazione QR code", http.StatusInternalServerError)
			return
		}

		data := struct {
			Username        string
			IsAdmin         bool
			Title           string
			ContentTemplate string
			Permissions     map[string]bool
			IsMultiCPU      bool
			CSRFField       template.HTML
			CSRFToken       string
			TOTPEnabled     bool
			TOTPSecret      bool
			QRCodeData      string
			Secret          string
			MFAEnabled      bool
		}{
			Username:        username,
			IsAdmin:         isAdmin,
			Title:           "Configura MFA",
			ContentTemplate: "mfaConfigureContent",
			Permissions:     perms,
			IsMultiCPU:      isMultiCPU(),
			CSRFField:       csrf.TemplateField(r),
			CSRFToken:       csrf.Token(r),
			TOTPEnabled:     u.TOTPEnabled,
			TOTPSecret:      u.TOTPSecret != "",
			QRCodeData:      base64.StdEncoding.EncodeToString(qrBytes),
			Secret:          secret,
			MFAEnabled:      config.MFAEnabled,
		}
		tmpl.ExecuteTemplate(w, "layout.html", data)
		return
	}

	if action == "verify" {
		code := r.FormValue("code")
		if code == "" {
			http.Error(w, "Codice mancante", http.StatusBadRequest)
			return
		}
		session, _ := store.Get(r, "portal-session")
		secret, ok := session.Values["totp_secret_temp"].(string)
		if !ok || secret == "" {
			http.Error(w, "Secret non trovato, riprova a generare il QR code", http.StatusBadRequest)
			return
		}
		if VerifyTOTP(secret, code) {
			userMutex.Lock()
			u.TOTPSecret = secret
			u.TOTPEnabled = true
			u.TOTPForceSetup = false
			u.MFAFailedAttempts = 0
			u.MFALockedUntil = time.Time{}
			backupCodes := generateBackupCodes(5)
			u.TOTPBackupCodes = backupCodes
			saveUsers(currentDataDir)
			userMutex.Unlock()

			delete(session.Values, "totp_secret_temp")
			delete(session.Values, "totp_url")
			session.Save(r, w)

			data := struct {
				Username        string
				IsAdmin         bool
				Title           string
				ContentTemplate string
				Permissions     map[string]bool
				IsMultiCPU      bool
				CSRFField       template.HTML
				CSRFToken       string
				BackupCodes     []string
				MFAEnabled      bool
			}{
				Username:        username,
				IsAdmin:         isAdmin,
				Title:           "MFA attivato",
				ContentTemplate: "mfaBackupCodesContent",
				Permissions:     perms,
				IsMultiCPU:      isMultiCPU(),
				CSRFField:       csrf.TemplateField(r),
				CSRFToken:       csrf.Token(r),
				BackupCodes:     backupCodes,
				MFAEnabled:      config.MFAEnabled,
			}
			tmpl.ExecuteTemplate(w, "layout.html", data)

			go func(userName string) {
				usersPath := filepath.Join(currentDataDir, "users.enc")
				if err := SyncFileToAllRemotes(usersPath); err != nil {
					log.Printf("❌ Errore sincronizzazione utenti (attivazione MFA per %s): %v", userName, err)
				} else {
					log.Printf("✅ Utenti sincronizzati dopo attivazione MFA di '%s'", userName)
				}
			}(username)

			WriteAuditLog("MFA_ENABLE", username, "MFA attivato con successo")
			return
		}
		http.Error(w, "Codice non valido", http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, "/profile/mfa", http.StatusFound)
}

// ===== HANDLER DISATTIVAZIONE MFA (con verifica password) =====
func mfaDisableHandler(w http.ResponseWriter, r *http.Request) {
	if !config.MFAEnabled {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	username, _ := getUserContext(r)
	u := getUserByUsername(username)
	if u == nil {
		http.Error(w, "Utente non trovato", http.StatusNotFound)
		return
	}

	password := r.FormValue("password")
	if password == "" {
		http.Error(w, "Password richiesta per disattivare MFA", http.StatusBadRequest)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		WriteAuditLog("mfa_disable_failed", username, "Tentativo di disattivazione MFA con password errata")
		http.Error(w, "Password errata", http.StatusUnauthorized)
		return
	}

	userMutex.Lock()
	u.TOTPEnabled = false
	u.TOTPSecret = ""
	u.TOTPBackupCodes = []string{}
	u.TOTPForceSetup = false
	u.MFAFailedAttempts = 0
	u.MFALockedUntil = time.Time{}
	saveUsers(currentDataDir)
	userMutex.Unlock()

	go func() {
		usersPath := filepath.Join(currentDataDir, "users.enc")
		if err := SyncFileToAllRemotes(usersPath); err != nil {
			log.Printf("❌ Errore sincronizzazione disattivazione MFA per %s: %v", username, err)
		} else {
			log.Printf("✅ MFA disattivato per %s e sincronizzato", username)
		}
	}()

	WriteAuditLog("mfa_disable_success", username, "MFA disattivato volontariamente")
	http.Redirect(w, r, "/profile/mfa", http.StatusFound)
}

// ===== RIGENERAZIONE CODICI DI BACKUP =====
func mfaRegenerateBackupHandler(w http.ResponseWriter, r *http.Request) {
	if !config.MFAEnabled {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	username, _ := getUserContext(r)
	u := getUserByUsername(username)
	if u == nil {
		http.Error(w, "Utente non trovato", http.StatusNotFound)
		return
	}

	if !u.TOTPEnabled {
		http.Error(w, "MFA non è attivo per questo utente", http.StatusBadRequest)
		return
	}

	newCodes := generateBackupCodes(5)

	userMutex.Lock()
	u.TOTPBackupCodes = newCodes
	saveUsers(currentDataDir)
	userMutex.Unlock()

	go func() {
		usersPath := filepath.Join(currentDataDir, "users.enc")
		if err := SyncFileToAllRemotes(usersPath); err != nil {
			log.Printf("❌ Errore sincronizzazione rigenerazione backup per %s: %v", username, err)
		} else {
			log.Printf("✅ Backup codes rigenerati per %s e sincronizzati", username)
		}
	}()

	WriteAuditLog("mfa_backup_regenerate", username, "Codici di backup rigenerati")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"backup_codes": newCodes,
		"message":      "Codici di backup rigenerati con successo",
	})
}

// ===== PAGINA STANDALONE PER VERIFICA MFA DURANTE LOGIN =====
func mfaLoginPageHandler(w http.ResponseWriter, r *http.Request) {
	if !config.MFAEnabled {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	session, _ := store.Get(r, "portal-session")
	username, ok := session.Values["mfa_pending_user"].(string)
	if !ok || username == "" {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	errMsg := ""
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		switch errParam {
		case "codice_non_valido":
			errMsg = "Codice non valido, riprova."
		case "codice_mancante":
			errMsg = "Inserisci il codice a 6 cifre."
		case "bloccato":
			errMsg = "Troppi tentativi falliti. Account bloccato per 15 minuti."
		default:
			errMsg = "Errore durante la verifica."
		}
	}

	data := struct {
		CSRFField template.HTML
		CSRFToken string
		Error     string
	}{
		CSRFField: csrf.TemplateField(r),
		CSRFToken: csrf.Token(r),
		Error:     errMsg,
	}
	tmpl.ExecuteTemplate(w, "mfa_login.html", data)
}

// ===== VERIFICA MFA DURANTE LOGIN (POST) =====
func mfaLoginVerifyHandler(w http.ResponseWriter, r *http.Request) {
	if !config.MFAEnabled {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	code := r.FormValue("code")
	if code == "" {
		http.Redirect(w, r, "/mfa-login?error=codice_mancante", http.StatusFound)
		return
	}

	session, _ := store.Get(r, "portal-session")
	username, ok := session.Values["mfa_pending_user"].(string)
	if !ok || username == "" {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	u := getUserByUsername(username)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	// ═══════════════════════════════════════════════════════
	// 🔒 CONTROLLO BLOCCO PER TENTATIVI FALLITI MFA
	// ═══════════════════════════════════════════════════════
	userMutex.Lock()
	if !u.MFALockedUntil.IsZero() {
		if time.Now().Before(u.MFALockedUntil) {
			userMutex.Unlock()
			log.Printf("🔒 Tentativo MFA durante blocco per %s", username)
			WriteAuditLog("mfa_login_blocked", username, "Tentativo di MFA durante blocco")
			http.Redirect(w, r, "/mfa-login?error=bloccato", http.StatusFound)
			return
		} else {
			// Il blocco è scaduto: resetta i tentativi
			u.MFAFailedAttempts = 0
			u.MFALockedUntil = time.Time{}
			saveUsers(currentDataDir)
			log.Printf("🔄 Blocco MFA scaduto per %s, tentativi resettati", username)
		}
	}
	userMutex.Unlock()

	// ═══════════════════════════════════════════════════════
	// VERIFICA CODICE TOTP O CODICE DI BACKUP
	// ═══════════════════════════════════════════════════════
	valid := false
	if VerifyTOTP(u.TOTPSecret, code) {
		valid = true
	} else {
		// Controlla se è un codice di backup
		for i, backupCode := range u.TOTPBackupCodes {
			if backupCode == code {
				valid = true
				// Rimuovi il codice di backup usato
				u.TOTPBackupCodes = append(u.TOTPBackupCodes[:i], u.TOTPBackupCodes[i+1:]...)
				userMutex.Lock()
				saveUsers(currentDataDir)
				userMutex.Unlock()
				WriteAuditLog("mfa_backup_used", username, "Codice di backup usato per il login")
				break
			}
		}
	}

	if valid {
		// ✅ MFA RIUSCITO: resetta i tentativi
		userMutex.Lock()
		u.MFAFailedAttempts = 0
		u.MFALockedUntil = time.Time{}
		saveUsers(currentDataDir)
		userMutex.Unlock()

		delete(session.Values, "mfa_pending_user")
		delete(session.Values, "mfa_pending_authenticated")
		session.Values["authenticated"] = true
		session.Values["username"] = username
		session.Values["is_admin"] = (u.Role == RoleAdmin)
		now := time.Now().Unix()
		session.Values["last_activity"] = now
		session.Values["created_at"] = now
		session.Save(r, w)

		log.Printf("✅ Login MFA riuscito per %s", username)
		WriteAuditLog("login_mfa_success", username, "Login con MFA riuscito")
		http.Redirect(w, r, "/alarms", http.StatusFound)
		return
	}

	// ❌ MFA FALLITO: incrementa il contatore
	userMutex.Lock()
	u.MFAFailedAttempts++
	locked := false
	if u.MFAFailedAttempts >= 5 {
		u.MFALockedUntil = time.Now().Add(15 * time.Minute)
		locked = true
		WriteAuditLog("mfa_login_locked", username, "Account bloccato per 15 minuti per troppi tentativi MFA falliti")
		log.Printf("🔒 Account %s bloccato per 15 minuti per troppi tentativi MFA falliti", username)
	}
	saveUsers(currentDataDir)
	userMutex.Unlock()

	ip := getClientIP(r)
	log.Printf("⚠️ Tentativo MFA fallito per %s da IP %s (codice: %s) - tentativo #%d", username, ip, code, u.MFAFailedAttempts)
	WriteAuditLog("login_mfa_failed", username, fmt.Sprintf("Tentativo di verifica MFA fallito da IP %s (#%d)", ip, u.MFAFailedAttempts))

	if locked {
		http.Redirect(w, r, "/mfa-login?error=bloccato", http.StatusFound)
	} else {
		http.Redirect(w, r, "/mfa-login?error=codice_non_valido", http.StatusFound)
	}
}