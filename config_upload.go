package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/csrf"
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
			CSRFField       template.HTML
			CSRFToken       string
		}{
			Username:        username,
			IsAdmin:         isAdmin,
			Title:           "Carica Configurazione",
			ContentTemplate: "configUploadContent",
			Message:         message,
			Permissions:     getUserPermissions(username),
			IsError:         isError,
			IsMultiCPU:      isMultiCPU(),
			CSRFField:       csrf.TemplateField(r),
			CSRFToken:       csrf.Token(r),
		}

		if err := tmpl.ExecuteTemplate(w, "layout.html", data); err != nil {
			log.Printf("❌ Errore rendering config upload: %v", err)
			http.Error(w, "Errore interno", http.StatusInternalServerError)
		}
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
	tempFile, err := ioutil.TempFile("", "upload_"+suffix)
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

	// 4. Validazione in base alla modalità (parziale o completa)
	partial := r.FormValue("partial") == "on"
	allowedExtensions := config.ExtensionFilesConfig

	if partial {
		// Modalità parziale: controlla solo le estensioni dei file presenti
		if len(allowedExtensions) > 0 {
			extSet, err := getArchiveExtensions(tempPath)
			if err != nil {
				log.Printf("Errore estrazione estensioni: %v", err)
				http.Redirect(w, r, "/config-upload?err=Errore lettura archivio", http.StatusFound)
				return
			}
			// Controlla che ogni estensione trovata sia nella lista consentita
			for ext := range extSet {
				found := false
				for _, allowed := range allowedExtensions {
					if ext == allowed {
						found = true
						break
					}
				}
				if !found {
					msg := "Estensione non consentita: " + ext + ". Estensioni permesse: " + strings.Join(allowedExtensions, ", ")
					http.Redirect(w, r, "/config-upload?err="+msg, http.StatusFound)
					return
				}
			}
			log.Printf("✅ Validazione parziale superata: %d estensioni trovate", len(extSet))
		} else {
			log.Printf("ℹ️ Nessuna estensione configurata in extension_files_config, salto controllo")
		}
	} else {
		// Modalità completa: richiede almeno un file per ogni tipo di estensione
		if len(allowedExtensions) > 0 {
			valid, err := validateArchiveContentRequired(tempPath, allowedExtensions)
			if err != nil {
				log.Printf("Errore validazione archivio: %v", err)
				http.Redirect(w, r, "/config-upload?err=Formato archivio non valido", http.StatusFound)
				return
			}
			if !valid {
				http.Redirect(w, r, "/config-upload?err=L'archivio non contiene tutti i tipi di file richiesti", http.StatusFound)
				return
			}
			log.Printf("✅ Validazione completa superata")
		} else {
			log.Printf("ℹ️ Nessuna estensione configurata, salto controllo completezza")
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

// ==================== FUNZIONI DI SUPPORTO PER LE ESTENSIONI (NUOVE) ====================

// getArchiveExtensions restituisce un set (map) delle estensioni dei file contenuti nell'archivio
func getArchiveExtensions(archivePath string) (map[string]bool, error) {
	ext := strings.ToLower(filepath.Ext(archivePath))
	switch ext {
	case ".zip":
		return getZipExtensions(archivePath)
	case ".tar":
		return getTarExtensions(archivePath)
	case ".gz":
		if strings.HasSuffix(strings.ToLower(archivePath), ".tar.gz") ||
			strings.HasSuffix(strings.ToLower(archivePath), ".tgz") {
			return getTarGzExtensions(archivePath)
		}
		return nil, fmt.Errorf("formato non supportato: %s", ext)
	default:
		return nil, fmt.Errorf("formato non supportato: %s", ext)
	}
}

func getZipExtensions(zipPath string) (map[string]bool, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	extSet := make(map[string]bool)
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(f.Name))
		if ext != "" {
			extSet[ext] = true
		}
	}
	return extSet, nil
}

func getTarExtensions(tarPath string) (map[string]bool, error) {
	f, err := os.Open(tarPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tr := tar.NewReader(f)
	extSet := make(map[string]bool)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag == tar.TypeReg {
			ext := strings.ToLower(filepath.Ext(header.Name))
			if ext != "" {
				extSet[ext] = true
			}
		}
	}
	return extSet, nil
}

func getTarGzExtensions(tarGzPath string) (map[string]bool, error) {
	f, err := os.Open(tarGzPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gzr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gzr.Close()
	tr := tar.NewReader(gzr)
	extSet := make(map[string]bool)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag == tar.TypeReg {
			ext := strings.ToLower(filepath.Ext(header.Name))
			if ext != "" {
				extSet[ext] = true
			}
		}
	}
	return extSet, nil
}

// ==================== FUNZIONI DI RILEVAMENTO TIPO FILE ====================

// detectFileType rileva il tipo di file dai magic bytes
func detectFileType(header []byte) string {
	if len(header) < 512 {
		return "unknown"
	}
	if len(header) >= 4 && string(header[0:4]) == "PK\x03\x04" {
		return "zip"
	}
	if len(header) >= 257 && string(header[257:262]) == "ustar" {
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