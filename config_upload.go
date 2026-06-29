package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
)

// MaxUploadSize è il limite massimo per i file di configurazione (2MB)
const MaxUploadSize = 2 << 20 // 2 MB

func configUploadHandler(w http.ResponseWriter, r *http.Request) {
	username, isAdmin := getUserContext(r)
	if !isAdmin {
		http.Error(w, "Accesso negato", http.StatusForbidden)
		return
	}

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

	// ---- LIMITE DIMENSIONE (2MB) ----
	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadSize)
	if err := r.ParseMultipartForm(MaxUploadSize); err != nil {
		log.Printf("Errore: file troppo grande o errore parsing: %v", err)
		http.Redirect(w, r, "/config-upload?err=Il file supera il limite massimo di 2MB", http.StatusFound)
		return
	}

	// 1. Leggi il file caricato
	file, handler, err := r.FormFile("configfile")
	if err != nil {
		log.Printf("Errore nel file: %v", err)
		http.Redirect(w, r, "/config-upload?err=Errore nel file", http.StatusFound)
		return
	}
	defer file.Close()

	// ---- VALIDAZIONE MAGIC BYTES ----
	header := make([]byte, 512)
	if _, err := file.Read(header); err != nil && err != io.EOF {
		log.Printf("Errore lettura header: %v", err)
		http.Redirect(w, r, "/config-upload?err=Errore lettura file", http.StatusFound)
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		log.Printf("Errore seek: %v", err)
		http.Redirect(w, r, "/config-upload?err=Errore lettura file", http.StatusFound)
		return
	}

	fileType := detectFileType(header)
	if !isValidArchiveType(fileType) {
		log.Printf("Tipo di file non valido: %s (estensione: %s)", fileType, handler.Filename)
		http.Redirect(w, r, "/config-upload?err=Tipo di file non valido. Sono supportati solo ZIP, TAR e TAR.GZ", http.StatusFound)
		return
	}

	// 2. Verifica directory configurazione corrente
	if config.CurrentConfigurationDir == "" {
		http.Redirect(w, r, "/config-upload?err=Directory configurazione corrente non configurata", http.StatusFound)
		return
	}
	if err := os.MkdirAll(config.CurrentConfigurationDir, 0755); err != nil {
		log.Printf("Errore creazione directory: %v", err)
		http.Redirect(w, r, "/config-upload?err=Errore creazione directory", http.StatusFound)
		return
	}

	// 3. Salva il file caricato in un file temporaneo con l'estensione corretta
	var suffix string
	switch fileType {
	case "zip":
		suffix = "*.zip"
	case "tar":
		suffix = "*.tar"
	case "tar.gz":
		suffix = "*.tar.gz"
	default:
		suffix = "*.tmp"
	}
	tempFile, err := os.CreateTemp("", "upload_"+suffix)
	if err != nil {
		log.Printf("Errore salvataggio temporaneo: %v", err)
		http.Redirect(w, r, "/config-upload?err=Errore salvataggio temporaneo", http.StatusFound)
		return
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	if _, err := io.Copy(tempFile, file); err != nil {
		tempFile.Close()
		log.Printf("Errore copia file: %v", err)
		http.Redirect(w, r, "/config-upload?err=Errore copia file", http.StatusFound)
		return
	}
	tempFile.Close()

	// 4. Validazione del contenuto dell'archivio (estensioni richieste)
	requiredExtensions := config.ExtensionFilesConfig
	if len(requiredExtensions) > 0 {
		valid, err := validateArchiveContentRequired(tempPath, requiredExtensions)
		if err != nil {
			log.Printf("Errore validazione archivio: %v", err)
			http.Redirect(w, r, "/config-upload?err=Formato archivio non valido", http.StatusFound)
			return
		}
		if !valid {
			http.Redirect(w, r, "/config-upload?err=L'archivio non contiene tutti i tipi di file richiesti", http.StatusFound)
			return
		}
	}

	// 5. Backup dell'attuale directory corrente
	backupPath, backupName, err := backupCurrentConfigDir()
	if err != nil {
		log.Printf("Errore backup: %v", err)
		http.Redirect(w, r, "/config-upload?err=Errore durante il backup della configurazione corrente", http.StatusFound)
		return
	}
	log.Printf("Backup automatico creato: %s", backupName)

	// 6. Estrai l'archivio nella directory corrente (con protezione Zip Slip)
	if err := extractArchive(tempPath, config.CurrentConfigurationDir); err != nil {
		log.Printf("Errore estrazione: %v", err)
		http.Redirect(w, r, "/config-upload?err=Errore durante l'estrazione dell'archivio", http.StatusFound)
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

// ==================== HELPER PER LA VALIDAZIONE DEI FILE ====================

// detectFileType rileva il tipo di file dai magic bytes
func detectFileType(header []byte) string {
	if len(header) < 512 {
		return "unknown"
	}
	if len(header) >= 4 && bytes.Equal(header[0:4], []byte("PK\x03\x04")) {
		return "zip"
	}
	if len(header) >= 257 && bytes.Equal(header[257:262], []byte("ustar")) {
		return "tar"
	}
	if len(header) >= 2 && header[0] == 0x1F && header[1] == 0x8B {
		return "tar.gz"
	}
	return "unknown"
}

// isValidArchiveType verifica se il tipo è supportato
func isValidArchiveType(fileType string) bool {
	switch fileType {
	case "zip", "tar", "tar.gz":
		return true
	default:
		return false
	}
}
