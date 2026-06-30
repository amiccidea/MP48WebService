package main

import (
	"io/fs"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/csrf"
)

func isMultiCPU() bool {
	remoteCount := 0
	for _, m := range config.RemoteMachines {
		if m.ID != "local" {
			remoteCount++
		}
	}
	return remoteCount > 0
}
func main() {
	// Serve file statici
	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatal("Errore nel servire file statici:", err)
	}
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	// Route pubbliche
	http.HandleFunc("/login", loginHandler)
	http.HandleFunc("/change-password", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			changePasswordPage(w, r)
		} else if r.Method == http.MethodPost {
			changePasswordPost(w, r)
		} else {
			http.Error(w, "Metodo non consentito", http.StatusMethodNotAllowed)
		}
	})

	// Route protette
	http.HandleFunc("/alarms", authMiddleware(permissionMiddleware(PermAlarms)(alarmsHandler)))
	http.HandleFunc("/api/alarms", authMiddleware(apiAlarmsHandler))
	http.HandleFunc("/analog-inputs", authMiddleware(permissionMiddleware(PermAnalogInputs)(analogInputsPage)))
	http.HandleFunc("/api/analog-inputs", authMiddleware(apiAnalogInputsHandler))
	http.HandleFunc("/logout", authMiddleware(logoutHandler))
	http.HandleFunc("/logs", authMiddleware(logsPageHandler))
	http.HandleFunc("/api/logs", authMiddleware(apiLogsHandler))
	http.HandleFunc("/logs/download", authMiddleware(logsDownloadHandler))
	http.HandleFunc("/logs/delete", authMiddleware(adminMiddleware(logsDeleteHandler)))
	http.HandleFunc("/machine-status", authMiddleware(machineStatusHandler))
	http.HandleFunc("/config-history", authMiddleware(permissionMiddleware(PermConfigHistory)(configHistoryHandler)))
	http.HandleFunc("/config-history/download/", authMiddleware(adminMiddleware(configHistoryDownloadHandler)))
	http.HandleFunc("/config-history/delete/", authMiddleware(adminMiddleware(configHistoryDeleteHandler)))
	http.HandleFunc("/config-current/download", authMiddleware(permissionMiddleware(PermConfigHistory)(configCurrentFileDownloadHandler)))
	http.HandleFunc("/config-history/restore/", authMiddleware(adminMiddleware(configHistoryRestoreHandler)))
	http.HandleFunc("/config-upload", authMiddleware(adminMiddleware(configUploadHandler)))

	// Route admin
	http.HandleFunc("/admin/users", authMiddleware(adminMiddleware(adminUsersPage)))
	http.HandleFunc("/admin/users/create", authMiddleware(adminMiddleware(adminUserCreate)))
	http.HandleFunc("/admin/users/delete", authMiddleware(adminMiddleware(adminUserDelete)))
	http.HandleFunc("/admin/users/edit", authMiddleware(adminMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			adminUserEditForm(w, r)
		} else if r.Method == http.MethodPost {
			adminUserEditPost(w, r)
		} else {
			http.Error(w, "Metodo non consentito", http.StatusMethodNotAllowed)
		}
	})))
	http.HandleFunc("/admin/settings", authMiddleware(adminMiddleware(adminSettingsPage)))
	http.HandleFunc("/admin/settings/save", authMiddleware(adminMiddleware(adminSettingsSave)))

	// Gestione ruoli
	http.HandleFunc("/admin/roles", authMiddleware(adminMiddleware(adminRolesPage)))
	http.HandleFunc("/admin/roles/create", authMiddleware(adminMiddleware(adminRolesCreate)))
	http.HandleFunc("/admin/roles/delete", authMiddleware(adminMiddleware(adminRolesDelete)))
	http.HandleFunc("/admin/roles/update", authMiddleware(adminMiddleware(adminRolesUpdate)))

	// Cambio password volontario
	http.HandleFunc("/profile/change-password", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			profileChangePasswordPage(w, r)
		} else if r.Method == http.MethodPost {
			profileChangePasswordPost(w, r)
		} else {
			http.Error(w, "Metodo non consentito", http.StatusMethodNotAllowed)
		}
	}))

	// Sblocco utente
	http.HandleFunc("/admin/users/unlock", authMiddleware(adminMiddleware(adminUserUnlock)))

	// Reboot
	http.HandleFunc("/reboot", authMiddleware(adminMiddleware(rebootPageHandler)))
	http.HandleFunc("/api/reboot-local", authMiddleware(adminMiddleware(rebootLocalHandler)))
	http.HandleFunc("/api/reboot-remote", authMiddleware(adminMiddleware(rebootRemoteHandler)))
	http.HandleFunc("/api/reboot-cascade", authMiddleware(adminMiddleware(rebootCascadeHandler)))
	http.HandleFunc("/api/reboot-cascade-all", authMiddleware(adminMiddleware(rebootCascadeAllHandler)))
	http.HandleFunc("/api/reboot-status", authMiddleware(adminMiddleware(rebootStatusHandler)))

	http.HandleFunc("/admin/remote-credentials", authMiddleware(adminMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			remoteCredentialsPageHandler(w, r)
		} else if r.Method == http.MethodPost {
			remoteCredentialsSaveHandler(w, r)
		} else {
			http.Error(w, "Metodo non consentito", http.StatusMethodNotAllowed)
		}
	})))

	// Sincronizzazione manuale
	http.HandleFunc("/sync", authMiddleware(adminMiddleware(syncPageHandler)))
	http.HandleFunc("/api/sync-remotes", authMiddleware(adminMiddleware(syncAllRemotesHandler)))
	http.HandleFunc("/api/sync-events", authMiddleware(adminMiddleware(syncEventsHandler)))
	http.HandleFunc("/api/sync-audit-log", authMiddleware(adminMiddleware(SyncAuditLogNowHandler)))

	// Redirect home
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/alarms", http.StatusFound)
	})

	// Avvolgi il router principale con il middleware CSRF
	// Usa la stessa chiave di sessione per CSRF (o una separata)
	csrfMiddleware := csrf.Protect(
		[]byte(config.SessionSecret),          // usa la stessa chiave
		csrf.Secure(config.TLSCertFile != ""), // secure solo se HTTPS è abilitato
		csrf.Path("/"),
		csrf.ErrorHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "CSRF token non valido", http.StatusForbidden)
		})),
	)

	// Avvia i server applicando il middleware al router principale
	// Se usi http.DefaultServeMux, avvolgi tutto:
	handler := csrfMiddleware(http.DefaultServeMux)

	// ---------- Avvio server con redirect HTTPS ----------
	if config.TLSCertFile != "" && config.TLSKeyFile != "" {
		// Avvia il server HTTP che fa solo redirect a HTTPS
		go func() {
			log.Printf("🔄 Server HTTP (redirect) avviato su http://localhost:%s", config.Port)
			if err := http.ListenAndServe(":"+config.Port, http.HandlerFunc(redirectToHTTPS)); err != nil {
				log.Fatalf("Errore server HTTP (redirect): %v", err)
			}
		}()

		// Avvia il server HTTPS
		log.Printf("🔒 Server HTTPS avviato su https://localhost:%s", config.PortSSL)
		if err := http.ListenAndServeTLS(":"+config.PortSSL, config.TLSCertFile, config.TLSKeyFile, handler); err != nil {
			log.Fatalf("Errore server HTTPS: %v", err)
		}
	} else {
		// Nessun certificato: solo HTTP
		log.Printf("🌐 Server HTTP avviato su http://localhost:%s", config.Port)
		if err := http.ListenAndServe(":"+config.Port, handler); err != nil {
			log.Fatalf("Errore server HTTP: %v", err)
		}
	}
}

// redirectToHTTPS effettua il redirect a HTTPS mantenendo path e query
func redirectToHTTPS(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	// Se la porta non è 443, la aggiungiamo
	if config.PortSSL != "" && config.PortSSL != "443" {
		// Rimuovi eventuale porta esistente
		host = strings.Split(host, ":")[0]
		host = host + ":" + config.PortSSL
	}
	target := "https://" + host + r.URL.Path
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, target, http.StatusMovedPermanently)
}
