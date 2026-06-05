package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
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

		// 2. Verifica che la directory della configurazione corrente sia configurata
		if config.CurrentConfigurationDir == "" {
			http.Error(w, "Directory configurazione corrente non configurata", http.StatusInternalServerError)
			return
		}

		// 3. Trova il file di configurazione corrente (supponendo un solo file)
		entries, err := os.ReadDir(config.CurrentConfigurationDir)
		if err != nil {
			log.Printf("Errore lettura directory corrente: %v", err)
			http.Error(w, "Errore accesso directory corrente", http.StatusInternalServerError)
			return
		}
		if len(entries) == 0 {
			http.Error(w, "Nessun file di configurazione corrente trovato", http.StatusNotFound)
			return
		}
		currentConfigPath := filepath.Join(config.CurrentConfigurationDir, entries[0].Name())

		// 4. Crea un backup della configurazione corrente prima di sovrascriverla
		timestamp := time.Now().Format("20060102_150405")
		backupName := fmt.Sprintf("%s_backup_%s.rcd",
			strings.TrimSuffix(entries[0].Name(), filepath.Ext(entries[0].Name())),
			timestamp)
		backupPath := filepath.Join(config.ConfigHistoryDir, backupName)
		if err := copyFile(currentConfigPath, backupPath); err != nil {
			log.Printf("Errore backup configurazione corrente: %v", err)
			http.Error(w, "Errore durante il backup della configurazione corrente", http.StatusInternalServerError)
			return
		}

		// 5. Sovrascrive la configurazione corrente con il file caricato
		dst, err := os.Create(currentConfigPath)
		if err != nil {
			log.Printf("Errore creazione file destinazione: %v", err)
			http.Error(w, "Errore salvataggio", http.StatusInternalServerError)
			return
		}
		defer dst.Close()
		if _, err := io.Copy(dst, file); err != nil {
			log.Printf("Errore copia file: %v", err)
			http.Error(w, "Errore scrittura file", http.StatusInternalServerError)
			return
		}

		// 6. Log dell'operazione
		WriteAuditLog("CONFIG_UPLOAD", username, fmt.Sprintf("caricato file %s (backup automatico: %s)", handler.Filename, backupName))

		// 7. Mostra la pagina con messaggio di successo
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
			Message:         "File caricato con successo. Backup automatico creato: " + backupName,
			Permissions:     getUserPermissions(username),
		}
		tmpl.ExecuteTemplate(w, "layout.html", data)
		return
	}
}
