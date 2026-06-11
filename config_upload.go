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

	// Helper per visualizzare la pagina con messaggio (errore o successo)
	renderPage := func(message string, isError bool) {
		data := struct {
			Username        string
			IsAdmin         bool
			Title           string
			ContentTemplate string
			Message         string
			Permissions     map[string]bool
			IsError         bool
		}{
			Username:        username,
			IsAdmin:         isAdmin,
			Title:           "Carica Configurazione",
			ContentTemplate: "configUploadContent",
			Message:         message,
			Permissions:     getUserPermissions(username),
			IsError:         isError,
		}
		tmpl.ExecuteTemplate(w, "layout.html", data)
	}

	if r.Method == http.MethodGet {
		renderPage("", false)
		return
	}

	if r.Method == http.MethodPost {
		// 1. Leggi il file caricato
		file, handler, err := r.FormFile("configfile")
		if err != nil {
			renderPage("Errore nel file: "+err.Error(), true)
			return
		}
		defer file.Close()

		// 2. Verifica directory configurazione corrente
		if config.CurrentConfigurationDir == "" {
			renderPage("Directory configurazione corrente non configurata", true)
			return
		}
		if err := os.MkdirAll(config.CurrentConfigurationDir, 0755); err != nil {
			renderPage("Errore creazione directory: "+err.Error(), true)
			return
		}

		// 3. Salva il file caricato in un file temporaneo
		tempFile, err := os.CreateTemp("", "upload_*.zip")
		if err != nil {
			renderPage("Errore salvataggio temporaneo: "+err.Error(), true)
			return
		}
		tempPath := tempFile.Name()
		defer os.Remove(tempPath)
		if _, err := io.Copy(tempFile, file); err != nil {
			tempFile.Close()
			renderPage("Errore copia file: "+err.Error(), true)
			return
		}
		tempFile.Close()

		// 4. Validazione del contenuto dell'archivio
		requiredExtensions := config.ExtensionFilesConfig
		if len(requiredExtensions) == 0 {
			// Se non ci sono estensioni configurate, nessuna validazione
			requiredExtensions = []string{}
		}
		valid, err := validateArchiveContentRequired(tempPath, requiredExtensions)
		if err != nil {
			log.Printf("Errore validazione archivio: %v", err)
			renderPage("Formato archivio non valido o danneggiato: "+err.Error(), true)
			return
		}
		if !valid {
			renderPage(fmt.Sprintf("L'archivio non contiene tutti i tipi di file richiesti: %v", requiredExtensions), true)
			return
		}

		// 5. Backup dell'attuale directory corrente
		_, backupName, err := backupCurrentConfigDir()
		if err != nil {
			log.Printf("Errore backup: %v", err)
			renderPage("Errore durante il backup della configurazione corrente: "+err.Error(), true)
			return
		}
		log.Printf("Backup automatico creato: %s", backupName)

		// 6. Estrai l'archivio nella directory corrente
		if err := extractArchive(tempPath, config.CurrentConfigurationDir); err != nil {
			log.Printf("Errore estrazione: %v", err)
			renderPage("Errore durante l'estrazione dell'archivio: "+err.Error(), true)
			return
		}

		// 7. Log dell'operazione
		WriteAuditLog("CONFIG_UPLOAD", username, fmt.Sprintf("caricato archivio %s (backup automatico: %s)", handler.Filename, backupName))

		// 8. Mostra successo
		renderPage("Archivio caricato ed estratto con successo. Backup automatico creato: "+backupName, false)
		return
	}
}
