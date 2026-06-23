package main

import (
	"log"
	"net/http"
)

func remoteCredentialsPageHandler(w http.ResponseWriter, r *http.Request) {
	username, isAdmin := getUserContext(r)
	if username == "" || !isAdmin {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	// Carica le credenziali attuali (se esistenti)
	creds, err := loadRemoteCredentials(currentDataDir)
	if err != nil {
		// Se il file non esiste, inizializza una mappa vuota
		creds = &RemoteCredentials{Machines: make(map[string]RemoteCredential)}
	}
	if creds == nil || creds.Machines == nil {
		creds = &RemoteCredentials{Machines: make(map[string]RemoteCredential)}
	}

	// Assicura che tutte le macchine configurate abbiano una voce nella mappa
	for _, m := range config.RemoteMachines {
		if _, ok := creds.Machines[m.ID]; !ok {
			creds.Machines[m.ID] = RemoteCredential{}
		}
	}

	perms := getUserPermissions(username)
	data := struct {
		Username        string
		IsAdmin         bool
		Title           string
		ContentTemplate string
		Permissions     map[string]bool
		RemoteMachines  []RemoteMachine
		Credentials     map[string]RemoteCredential
		HasCredentials  bool
		IsMultiCPU      bool
	}{
		Username:        username,
		IsAdmin:         isAdmin,
		Title:           "Credenziali Remote",
		ContentTemplate: "remoteCredentialsContent",
		Permissions:     perms,
		RemoteMachines:  config.RemoteMachines,
		Credentials:     creds.Machines,
		HasCredentials:  len(creds.Machines) > 0,
		IsMultiCPU:      isMultiCPU(),
	}
	tmpl.ExecuteTemplate(w, "layout.html", data)
}

func remoteCredentialsSaveHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_, isAdmin := getUserContext(r)
	if !isAdmin {
		http.Error(w, "Accesso negato", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Errore parsing form", http.StatusBadRequest)
		return
	}

	creds := &RemoteCredentials{
		Machines: make(map[string]RemoteCredential),
	}

	for _, m := range config.RemoteMachines {
		id := m.ID
		cred := RemoteCredential{
			FTPUsername:    r.FormValue("ftp_username_" + id),
			FTPPassword:    r.FormValue("ftp_password_" + id),
			TelnetUsername: r.FormValue("telnet_username_" + id),
			TelnetPassword: r.FormValue("telnet_password_" + id),
			SudoPassword:   r.FormValue("sudo_password_" + id),
		}
		creds.Machines[id] = cred
	}

	if err := saveRemoteCredentials(currentDataDir, creds); err != nil {
		log.Printf("Errore salvataggio credenziali remote: %v", err)
		http.Error(w, "Errore salvataggio", http.StatusInternalServerError)
		return
	}
	// Sincronizza asincrono
	go func() {
		// Sincronizza l'intera directory data (dove stanno remote_creds.enc)
		if err := SyncDirToAllRemotes(currentDataDir); err != nil {
			log.Printf("Errore sincronizzazione directory data: %v", err)
		}
	}()

	// Aggiorna la variabile globale (per il caricamento immediato)
	remoteCreds, _ = loadRemoteCredentials(currentDataDir)

	http.Redirect(w, r, "/admin/remote-credentials?success=true", http.StatusFound)
}
