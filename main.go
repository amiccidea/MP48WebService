package main

import (
	"io/fs"
	"log"
	"net/http"
)

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

	http.HandleFunc("/api/sync-remotes", authMiddleware(adminMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Sincronizza entrambe le directory (data e configurazione)
		go func() {
			if err := SyncDirToAllRemotes(currentDataDir); err != nil {
				log.Printf("Errore sync data: %v", err)
			}
			if err := SyncDirToAllRemotes(config.CurrentConfigurationDir); err != nil {
				log.Printf("Errore sync config: %v", err)
			}
		}()
		w.Write([]byte("Sincronizzazione avviata in background"))
	})))

	http.HandleFunc("/admin/remote-credentials", authMiddleware(adminMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			remoteCredentialsPageHandler(w, r)
		} else if r.Method == http.MethodPost {
			remoteCredentialsSaveHandler(w, r)
		} else {
			http.Error(w, "Metodo non consentito", http.StatusMethodNotAllowed)
		}
	})))

	// Redirect home
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/alarms", http.StatusFound)
	})

	// Avvia server HTTP sulla porta config.Port
	go func() {
		log.Printf("Server HTTP avviato su http://localhost:%s", config.Port)
		if err := http.ListenAndServe(":"+config.Port, nil); err != nil {
			log.Fatalf("Errore server HTTP: %v", err)
		}
	}()

	// Avvia server HTTPS sulla porta config.PortSSL se i certificati sono configurati
	if config.TLSCertFile != "" && config.TLSKeyFile != "" {
		log.Printf("Server HTTPS avviato su https://localhost:%s", config.PortSSL)
		if err := http.ListenAndServeTLS(":"+config.PortSSL, config.TLSCertFile, config.TLSKeyFile, nil); err != nil {
			log.Fatalf("Errore server HTTPS: %v", err)
		}
	} else {
		log.Println("Certificati TLS non configurati, server HTTPS non avviato")
		// Mantiene il main in esecuzione
		select {}
	}
}
