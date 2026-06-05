package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Struttura per i dati del file corrente
type CurrentFileInfo struct {
	Name    string
	ModTime string
	Size    string
}

func configHistoryHandler(w http.ResponseWriter, r *http.Request) {
	username, isAdmin := getUserContext(r)
	perms := getUserPermissions(username)
	backupDir := config.ConfigHistoryDir
	if backupDir == "" {
		backupDir = "./config_history"
	}
	extensions := config.ConfigExtensions
	if len(extensions) == 0 {
		extensions = []string{".rdc"}
	}
	startDate := r.URL.Query().Get("start_date")
	endDate := r.URL.Query().Get("end_date")
	var startUnix, endUnix int64
	if startDate != "" {
		t, _ := time.Parse("2006-01-02", startDate)
		startUnix = t.Unix()
	}
	if endDate != "" {
		t, _ := time.Parse("2006-01-02", endDate)
		endUnix = t.Unix() + 86400 - 1
	} else {
		endUnix = time.Now().Unix()
	}
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		entries = []os.DirEntry{}
	}
	type FileInfo struct {
		Name       string
		ModTime    string
		ModTimeRaw time.Time
		Size       string
	}
	var files []FileInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		extOk := false
		for _, ext := range extensions {
			if strings.HasSuffix(strings.ToLower(name), strings.ToLower(ext)) {
				extOk = true
				break
			}
		}
		if !extOk {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		modTime := info.ModTime()
		modUnix := modTime.Unix()
		if startUnix > 0 && modUnix < startUnix {
			continue
		}
		if endUnix > 0 && modUnix > endUnix {
			continue
		}
		files = append(files, FileInfo{
			Name:       name,
			ModTime:    modTime.Format("2006-01-02 15:04:05"),
			ModTimeRaw: modTime,
			Size:       formatFileSize(info.Size()),
		})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTimeRaw.After(files[j].ModTimeRaw)
	})

	// --- Configurazione corrente (dati completi) ---
	type CurrentFileInfo struct {
		Name    string
		ModTime string
		Size    string
	}
	currentFile := CurrentFileInfo{}
	if config.CurrentConfigurationDir != "" {
		dirEntries, err := os.ReadDir(config.CurrentConfigurationDir)
		if err == nil {
			for _, entry := range dirEntries {
				if !entry.IsDir() {
					if info, err := entry.Info(); err == nil {
						currentFile = CurrentFileInfo{
							Name:    entry.Name(),
							ModTime: info.ModTime().Format("2006-01-02 15:04:05"),
							Size:    formatFileSize(info.Size()),
						}
					}
					break // prende solo il primo file
				}
			}
		}
	}

	data := struct {
		Username          string
		IsAdmin           bool
		Title             string
		ContentTemplate   string
		Files             []FileInfo
		StartDate         string
		EndDate           string
		Extensions        []string
		Permissions       map[string]bool
		CurrentConfigFile CurrentFileInfo
	}{
		Username:          username,
		IsAdmin:           isAdmin,
		Title:             "Storico Configurazioni",
		ContentTemplate:   "configHistoryContent",
		Files:             files,
		StartDate:         startDate,
		EndDate:           endDate,
		Extensions:        extensions,
		Permissions:       perms,
		CurrentConfigFile: currentFile,
	}
	tmpl.ExecuteTemplate(w, "layout.html", data)
}

func configHistoryDownloadHandler(w http.ResponseWriter, r *http.Request) {
	filename := strings.TrimPrefix(r.URL.Path, "/config-history/download/")
	if filename == "" {
		http.Error(w, "Nome file mancante", http.StatusBadRequest)
		return
	}
	filePath := filepath.Join(config.ConfigHistoryDir, filename)
	// Controlla che il file esista
	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		http.Error(w, "File non trovato", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "Errore interno", http.StatusInternalServerError)
		return
	}
	// Forza il download
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	http.ServeFile(w, r, filePath)
}

func configCurrentDownloadHandler(w http.ResponseWriter, r *http.Request) {
	if config.CurrentConfigurationDir == "" {
		http.Error(w, "Directory configurazione corrente non configurata", http.StatusNotFound)
		return
	}
	entries, err := os.ReadDir(config.CurrentConfigurationDir)
	if err != nil || len(entries) == 0 {
		http.Error(w, "Nessun file di configurazione corrente trovato", http.StatusNotFound)
		return
	}
	// Prende il primo file (si assume un solo file)
	filename := entries[0].Name()
	filePath := filepath.Join(config.CurrentConfigurationDir, filename)
	info, err := os.Stat(filePath)
	if err != nil {
		http.Error(w, "Errore lettura file", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	http.ServeFile(w, r, filePath)
}

func configHistoryDeleteHandler(w http.ResponseWriter, r *http.Request) {
	// Estrae il nome del file dall'URL
	filename := strings.TrimPrefix(r.URL.Path, "/config-history/delete/")
	if filename == "" {
		http.Error(w, "Nome file mancante", http.StatusBadRequest)
		return
	}
	// Protegge da path traversal (es. ../)
	if strings.Contains(filename, "..") {
		http.Error(w, "Percorso non valido", http.StatusBadRequest)
		return
	}
	filePath := filepath.Join(config.ConfigHistoryDir, filename)
	// Verifica che il file esista
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		http.Error(w, "File non trovato", http.StatusNotFound)
		return
	}
	// Elimina il file
	if err := os.Remove(filePath); err != nil {
		http.Error(w, "Errore durante l'eliminazione", http.StatusInternalServerError)
		return
	}
	// Log dell'operazione
	username, _ := getUserContext(r)
	WriteAuditLog("CONFIG_DELETE", username, filename)
	w.WriteHeader(http.StatusOK)
}

// configHistoryRestoreHandler gestisce il ripristino di un backup
func configHistoryRestoreHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Solo admin
	_, isAdmin := getUserContext(r)
	if !isAdmin {
		http.Error(w, "Accesso negato", http.StatusForbidden)
		return
	}

	// Estrai il nome del file dall'URL
	filename := strings.TrimPrefix(r.URL.Path, "/config-history/restore/")
	if filename == "" {
		http.Error(w, "Nome file mancante", http.StatusBadRequest)
		return
	}
	backupPath := filepath.Join(config.ConfigHistoryDir, filename)

	// Verifica che il file di backup esista
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		http.Error(w, "File di backup non trovato", http.StatusNotFound)
		return
	}

	// Verifica che la directory della configurazione corrente sia configurata
	if config.CurrentConfigurationDir == "" {
		http.Error(w, "Directory configurazione corrente non configurata", http.StatusInternalServerError)
		return
	}

	// Trova il file di configurazione corrente (supponendo un solo file nella directory)
	entries, err := os.ReadDir(config.CurrentConfigurationDir)
	if err != nil {
		http.Error(w, "Errore lettura directory corrente", http.StatusInternalServerError)
		return
	}
	if len(entries) == 0 {
		http.Error(w, "Nessun file di configurazione corrente trovato", http.StatusNotFound)
		return
	}
	currentConfigPath := filepath.Join(config.CurrentConfigurationDir, entries[0].Name())

	// Crea un backup della configurazione corrente prima di sovrascriverla
	timestamp := time.Now().Format("20060102_150405")
	backupName := fmt.Sprintf("%s_backup_%s.rcd", strings.TrimSuffix(entries[0].Name(), filepath.Ext(entries[0].Name())), timestamp)
	backupCopyPath := filepath.Join(config.ConfigHistoryDir, backupName)
	if err := copyFile(currentConfigPath, backupCopyPath); err != nil {
		log.Printf("Errore backup configurazione corrente: %v", err)
		http.Error(w, "Errore durante il backup della configurazione corrente", http.StatusInternalServerError)
		return
	}

	// Sovrascrive la configurazione corrente con il backup selezionato
	if err := copyFile(backupPath, currentConfigPath); err != nil {
		log.Printf("Errore ripristino file: %v", err)
		http.Error(w, "Errore durante il ripristino", http.StatusInternalServerError)
		return
	}

	// Log dell'operazione
	username, _ := getUserContext(r)
	WriteAuditLog("CONFIG_RESTORE", username, fmt.Sprintf("ripristinato backup %s come configurazione corrente (backup automatico creato: %s)", filename, backupName))

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// copyFile copia un file da src a dst
func copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()
	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()
	_, err = io.Copy(destination, source)
	return err
}
