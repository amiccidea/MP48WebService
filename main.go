package main

import (
	"io/fs"
	"log"
	"net/http"
)

func main() {
	// Serve statici
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
	//http.HandleFunc("/config-history", authMiddleware(adminMiddleware(configHistoryHandler)))
	http.HandleFunc("/config-history", authMiddleware(permissionMiddleware(PermConfigHistory)(configHistoryHandler)))
	http.HandleFunc("/config-history/download/", authMiddleware(adminMiddleware(configHistoryDownloadHandler)))
	http.HandleFunc("/config-history/delete/", authMiddleware(adminMiddleware(configHistoryDeleteHandler)))
	http.HandleFunc("/config-current/download", authMiddleware(permissionMiddleware(PermConfigHistory)(configCurrentDownloadHandler)))
	http.HandleFunc("/config-history/restore/", authMiddleware(adminMiddleware(configHistoryRestoreHandler)))
	/*http.HandleFunc("/config-history/download/", authMiddleware(adminMiddleware(func(w http.ResponseWriter, r *http.Request) {
		filename := strings.TrimPrefix(r.URL.Path, "/config-history/download/")
		filepath := config.ConfigHistoryDir + "/" + filename
		if _, err := os.Stat(filepath); os.IsNotExist(err) {
			http.Error(w, "File non trovato", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(filename))
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeFile(w, r, filepath)
	})))*/
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
	//unlock utente bloccato
	http.HandleFunc("/admin/users/unlock", authMiddleware(adminMiddleware(adminUserUnlock)))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
	})

	log.Printf("Server avviato su http://localhost:%s", config.Port)
	log.Fatal(http.ListenAndServe(":"+config.Port, nil))
}
