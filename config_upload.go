package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
)

func configUploadHandler(w http.ResponseWriter, r *http.Request) {
	username, isAdmin := getUserContext(r)
	if !isAdmin {
		http.Error(w, "Accesso negato", http.StatusForbidden)
		return
	}

	// Helper per visualizzare la pagina con messaggio (solo GET)
	renderPage := func(message string, isError bool) {
		data := struct {
			Username        string
			IsAdmin         bool
			Title           string
			ContentTemplate string
			Message         string
			Permissions     map[string]bool
			IsError         bool
			IsMultiCPU      bool
		}{
			Username:        username,
			IsAdmin:         isAdmin,
			Title:           "Carica Configurazione",
			ContentTemplate: "configUploadContent",
			Message:         message,
			Permissions:     getUserPermissions(username),
			IsError:         isError,
			IsMultiCPU:      isMultiCPU(),
		}
		tmpl.ExecuteTemplate(w, "layout.html", data)
	}

	if r.Method == http.MethodGet {
		// Controlla se ci sono messaggi dalla query string (dopo redirect)
		msg := r.URL.Query().Get("msg")
		errMsg := r.URL.Query().Get("err")
		if msg != "" {
			renderPage(msg, false)
		} else if errMsg != "" {
			renderPage(errMsg, true)
		} else {
			renderPage("", false)
		}
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// ---- PROCESSO IL FILE ----

	// 1. Leggi il file caricato
	file, handler, err := r.FormFile("configfile")
	if err != nil {
		log.Printf("Errore nel file: %v", err)
		http.Redirect(w, r, "/config-upload?err=Errore nel file: "+err.Error(), http.StatusFound)
		return
	}
	defer file.Close()

	// 2. Verifica directory configurazione corrente
	if config.CurrentConfigurationDir == "" {
		http.Redirect(w, r, "/config-upload?err=Directory configurazione corrente non configurata", http.StatusFound)
		return
	}
	if err := os.MkdirAll(config.CurrentConfigurationDir, 0755); err != nil {
		log.Printf("Errore creazione directory: %v", err)
		http.Redirect(w, r, "/config-upload?err=Errore creazione directory: "+err.Error(), http.StatusFound)
		return
	}

	// 3. Salva il file caricato in un file temporaneo
	tempFile, err := os.CreateTemp("", "upload_*.zip")
	if err != nil {
		log.Printf("Errore salvataggio temporaneo: %v", err)
		http.Redirect(w, r, "/config-upload?err=Errore salvataggio temporaneo: "+err.Error(), http.StatusFound)
		return
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)
	if _, err := io.Copy(tempFile, file); err != nil {
		tempFile.Close()
		log.Printf("Errore copia file: %v", err)
		http.Redirect(w, r, "/config-upload?err=Errore copia file: "+err.Error(), http.StatusFound)
		return
	}
	tempFile.Close()

	// 4. Validazione del contenuto dell'archivio
	requiredExtensions := config.ExtensionFilesConfig
	if len(requiredExtensions) > 0 {
		valid, err := validateArchiveContentRequired(tempPath, requiredExtensions)
		if err != nil {
			log.Printf("Errore validazione archivio: %v", err)
			http.Redirect(w, r, "/config-upload?err=Formato archivio non valido: "+err.Error(), http.StatusFound)
			return
		}
		if !valid {
			http.Redirect(w, r, "/config-upload?err=L'archivio non contiene tutti i tipi di file richiesti: "+fmt.Sprintf("%v", requiredExtensions), http.StatusFound)
			return
		}
	}

	// 5. Backup dell'attuale directory corrente
	backupPath, backupName, err := backupCurrentConfigDir()
	if err != nil {
		log.Printf("Errore backup: %v", err)
		http.Redirect(w, r, "/config-upload?err=Errore durante il backup della configurazione corrente: "+err.Error(), http.StatusFound)
		return
	}
	log.Printf("Backup automatico creato: %s", backupName)

	// 6. Estrai l'archivio nella directory corrente
	if err := extractArchive(tempPath, config.CurrentConfigurationDir); err != nil {
		log.Printf("Errore estrazione: %v", err)
		http.Redirect(w, r, "/config-upload?err=Errore durante l'estrazione dell'archivio: "+err.Error(), http.StatusFound)
		return
	}

	// 7. Sincronizza il backup sulle macchine remote (in background)
	go func() {
		if err := SyncFileToAllRemotes(backupPath); err != nil {
			log.Printf("Errore sincronizzazione backup %s: %v", backupName, err)
		} else {
			log.Printf("✅ Backup %s sincronizzato sulle macchine remote", backupName)
		}
	}()

	// 8. Sincronizza la configurazione sulle macchine remote (in background)
	go func() {
		if err := SyncDirToAllRemotes(config.CurrentConfigurationDir); err != nil {
			log.Printf("Errore sincronizzazione configurazione: %v", err)
		} else {
			log.Printf("✅ Configurazione sincronizzata sulle macchine remote")
		}
	}()

	// 9. Log dell'operazione
	WriteAuditLog("CONFIG_UPLOAD", username, fmt.Sprintf("caricato archivio %s (backup automatico: %s)", handler.Filename, backupName))

	// 10. Redirect con messaggio di successo
	msg := fmt.Sprintf("✅ Archivio caricato ed estratto con successo. Backup automatico creato: %s", backupName)
	http.Redirect(w, r, "/config-upload?msg="+msg, http.StatusFound)
}
