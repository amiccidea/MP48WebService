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

	if r.Method == http.MethodGet {
		data := struct {
			Username        string
			IsAdmin         bool
			Title           string
			ContentTemplate string
			Message         string
			Permissions     map[string]bool
		}{
			Username:        username,
			IsAdmin:         isAdmin,
			Title:           "Carica Configurazione",
			ContentTemplate: "configUploadContent",
			Message:         "",
			Permissions:     getUserPermissions(username),
		}
		tmpl.ExecuteTemplate(w, "layout.html", data)
		return
	}

	if r.Method == http.MethodPost {
		// 1. Leggi il file caricato
		file, handler, err := r.FormFile("configfile")
		if err != nil {
			http.Error(w, "Errore nel file", http.StatusBadRequest)
			return
		}
		defer file.Close()

		// 2. Verifica directory corrente
		if config.CurrentConfigurationDir == "" {
			http.Error(w, "Directory configurazione corrente non configurata", http.StatusInternalServerError)
			return
		}
		if err := os.MkdirAll(config.CurrentConfigurationDir, 0755); err != nil {
			http.Error(w, "Errore creazione directory", http.StatusInternalServerError)
			return
		}

		// 3. Salva il file caricato in temporaneo
		tempFile, err := os.CreateTemp("", "upload_*.zip")
		if err != nil {
			http.Error(w, "Errore salvataggio temporaneo", http.StatusInternalServerError)
			return
		}
		tempPath := tempFile.Name()
		defer os.Remove(tempPath)
		if _, err := io.Copy(tempFile, file); err != nil {
			tempFile.Close()
			http.Error(w, "Errore copia file", http.StatusInternalServerError)
			return
		}
		tempFile.Close()

		// 4. Backup dell'intera directory corrente (usa la funzione già definita in config_history.go)
		_, backupName, err := backupCurrentConfigDir()
		if err != nil {
			log.Printf("Errore backup: %v", err)
			http.Error(w, "Errore durante il backup della configurazione corrente", http.StatusInternalServerError)
			return
		}
		log.Printf("Backup automatico creato: %s", backupName)

		// 5. Estrai l'archivio nella directory corrente (sovrascrive, non cancella)
		if err := extractArchive(tempPath, config.CurrentConfigurationDir); err != nil {
			log.Printf("Errore estrazione: %v", err)
			http.Error(w, "Errore durante l'estrazione dell'archivio", http.StatusInternalServerError)
			return
		}

		// 6. Log
		WriteAuditLog("CONFIG_UPLOAD", username, fmt.Sprintf("caricato archivio %s (backup automatico: %s)", handler.Filename, backupName))

		// 7. Mostra successo
		data := struct {
			Username        string
			IsAdmin         bool
			Title           string
			ContentTemplate string
			Message         string
			Permissions     map[string]bool
		}{
			Username:        username,
			IsAdmin:         isAdmin,
			Title:           "Carica Configurazione",
			ContentTemplate: "configUploadContent",
			Message:         "Archivio caricato ed estratto con successo. Backup automatico creato: " + backupName,
			Permissions:     getUserPermissions(username),
		}
		tmpl.ExecuteTemplate(w, "layout.html", data)
		return
	}
}
