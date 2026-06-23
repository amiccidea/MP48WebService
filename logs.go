package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Variabili per la sincronizzazione dell'audit log
var (
	auditLogSyncMutex sync.Mutex
	auditLogLastSync  = make(map[string]time.Time) // path -> ultima modifica sincronizzata
)

func logsPageHandler(w http.ResponseWriter, r *http.Request) {
	username, isAdmin := getUserContext(r)
	perms := getUserPermissions(username)
	data := struct {
		Username        string
		IsAdmin         bool
		Title           string
		ContentTemplate string
		Permissions     map[string]bool
		IsMultiCPU      bool
	}{
		Username:        username,
		IsAdmin:         isAdmin,
		Title:           "Logs",
		ContentTemplate: "logsContent",
		Permissions:     perms,
		IsMultiCPU:      isMultiCPU(),
	}
	tmpl.ExecuteTemplate(w, "layout.html", data)
}

func scanAllLogs() ([]LogFileInfo, error) {
	var allLogs []LogFileInfo

	// Scansiona le categorie configurate
	for _, cat := range config.LogCategories {
		for _, dir := range cat.Directories {
			logs, err := scanDirectory(dir, cat.Name)
			if err != nil {
				log.Printf("Errore scansione %s: %v", dir, err)
				continue
			}
			allLogs = append(allLogs, logs...)
		}
	}

	// 🔄 Aggiungi sempre l'audit log directory (se configurata)
	if config.AuditLogDir != "" {
		// Verifica che non sia già inclusa (per evitare duplicati)
		alreadyIncluded := false
		for _, cat := range config.LogCategories {
			for _, dir := range cat.Directories {
				if dir == config.AuditLogDir {
					alreadyIncluded = true
					break
				}
			}
			if alreadyIncluded {
				break
			}
		}
		if !alreadyIncluded {
			logs, err := scanDirectory(config.AuditLogDir, "Audit Logs")
			if err != nil {
				log.Printf("Errore scansione audit log %s: %v", config.AuditLogDir, err)
			} else {
				allLogs = append(allLogs, logs...)
			}
		}
	}

	return allLogs, nil
}

func scanDirectory(dirPath, category string) ([]LogFileInfo, error) {
	var logs []LogFileInfo
	cleanPath := filepath.Clean(dirPath)
	err := filepath.WalkDir(cleanPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && isValidLogFile(d.Name()) {
			info, err := d.Info()
			if err != nil {
				return nil
			}
			logs = append(logs, LogFileInfo{
				Path:        path,
				Name:        d.Name(),
				Size:        formatFileSize(info.Size()),
				ModTime:     info.ModTime().Format("2006-01-02 15:04:05"),
				ModTimeUnix: info.ModTime().Unix(),
				Category:    category,
				Directory:   filepath.Base(filepath.Dir(path)),
			})
		}
		return nil
	})
	return logs, err
}

func isValidLogFile(filename string) bool {
	for _, ext := range config.LogExtensions {
		if strings.HasSuffix(strings.ToLower(filename), ext) {
			return true
		}
	}
	return false
}

func formatFileSize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	} else if size < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(size)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
}

func apiLogsHandler(w http.ResponseWriter, r *http.Request) {
	category := r.URL.Query().Get("category")
	startDate := r.URL.Query().Get("start_date")
	endDate := r.URL.Query().Get("end_date")
	pageStr := r.URL.Query().Get("page")
	pageSizeStr := r.URL.Query().Get("pageSize")
	page := 1
	pageSize := 50
	if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
		page = p
	}
	if ps, err := strconv.Atoi(pageSizeStr); err == nil && ps > 0 && ps <= 200 {
		pageSize = ps
	}
	allLogs, err := scanAllLogs()
	if err != nil {
		http.Error(w, "Errore scansione log", http.StatusInternalServerError)
		return
	}
	if category != "" && category != "all" {
		filtered := []LogFileInfo{}
		for _, l := range allLogs {
			if l.Category == category {
				filtered = append(filtered, l)
			}
		}
		allLogs = filtered
	}
	if startDate != "" || endDate != "" {
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
		filtered := []LogFileInfo{}
		for _, l := range allLogs {
			if (startUnix == 0 || l.ModTimeUnix >= startUnix) && (endUnix == 0 || l.ModTimeUnix <= endUnix) {
				filtered = append(filtered, l)
			}
		}
		allLogs = filtered
	}
	total := len(allLogs)
	totalPages := (total + pageSize - 1) / pageSize
	if page > totalPages && totalPages > 0 {
		page = totalPages
	}
	start := (page - 1) * pageSize
	end := start + pageSize
	if end > total {
		end = total
	}
	paginatedLogs := allLogs[start:end]
	log.Printf("Trovati %d log, pagina %d/%d (size=%d)", total, page, totalPages, pageSize)
	_, isAdmin := getUserContext(r)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"logs":       paginatedLogs,
		"categories": getCategoriesList(),
		"total":      total,
		"page":       page,
		"pageSize":   pageSize,
		"totalPages": totalPages,
		"isAdmin":    isAdmin,
	})
}

func getCategoriesList() []string {
	cats := []string{"all"}
	for _, cat := range config.LogCategories {
		cats = append(cats, cat.Name)
	}
	return cats
}

func logsDownloadHandler(w http.ResponseWriter, r *http.Request) {
	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		http.Error(w, "Percorso mancante", http.StatusBadRequest)
		return
	}
	allowed := false
	for _, cat := range config.LogCategories {
		for _, dir := range cat.Directories {
			if strings.HasPrefix(filepath.Clean(filePath), filepath.Clean(dir)) {
				allowed = true
				break
			}
		}
	}
	if !allowed {
		http.Error(w, "Accesso negato", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(filepath.Base(filePath)))
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, filePath)
}

// logsDeleteHandler elimina un file di log (solo admin) - con sincronizzazione eliminazione
func logsDeleteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	username, isAdmin := getUserContext(r)
	log.Printf("logsDeleteHandler: username=%s, isAdmin=%v", username, isAdmin)

	if !isAdmin {
		http.Error(w, "Accesso negato", http.StatusForbidden)
		return
	}

	filePath := r.FormValue("path")
	if filePath == "" {
		http.Error(w, "Percorso mancante", http.StatusBadRequest)
		return
	}
	log.Printf("Percorso ricevuto: %q", filePath)

	cleanPath := filepath.Clean(filePath)
	log.Printf("Percorso pulito: %q", cleanPath)

	// Verifica che il file sia in una directory consentita
	allowed := false
	for _, cat := range config.LogCategories {
		for _, dir := range cat.Directories {
			cleanDir := filepath.Clean(dir)
			if strings.HasPrefix(strings.ToLower(cleanPath), strings.ToLower(cleanDir)) {
				allowed = true
				break
			}
		}
	}
	if !allowed {
		log.Printf("Accesso negato: percorso %q non consentito", cleanPath)
		http.Error(w, "Accesso negato", http.StatusForbidden)
		return
	}

	// Elimina il file locale
	if err := os.Remove(cleanPath); err != nil {
		log.Printf("Errore eliminazione file %s: %v", cleanPath, err)
		http.Error(w, "Errore eliminazione file", http.StatusInternalServerError)
		return
	}

	WriteAuditLog("LOG_DELETE", username, fmt.Sprintf("eliminato file log %s", cleanPath))

	// 🔄 Sincronizza l'eliminazione del log sulle macchine remote (in background)
	go func(path string) {
		if err := SyncFileDeleteFromAllRemotes(path); err != nil {
			log.Printf("❌ Errore sincronizzazione eliminazione log %s: %v", path, err)
		} else {
			log.Printf("✅ Eliminazione log %s sincronizzata sulle macchine remote", path)
		}
	}(cleanPath)

	w.WriteHeader(http.StatusOK)
}

// ==================== SINCRONIZZAZIONE AUDIT LOG ====================

// StartAuditLogSyncTicker avvia un ticker periodico per sincronizzare l'audit log
func StartAuditLogSyncTicker(intervalMinutes int) {
	if intervalMinutes <= 0 {
		intervalMinutes = 5 // default: 5 minuti
	}
	ticker := time.NewTicker(time.Duration(intervalMinutes) * time.Minute)
	go func() {
		log.Printf("🔄 Avviata sincronizzazione periodica dell'audit log (ogni %d minuti)", intervalMinutes)
		for range ticker.C {
			syncAuditLog()
		}
	}()
}

// syncAuditLog sincronizza il file di audit log corrente
func syncAuditLog() {
	if config.AuditLogDir == "" {
		log.Printf("⚠️ AuditLogDir non configurato, salto sincronizzazione")
		return
	}

	// Costruisci il nome del file di audit corrente
	filename := fmt.Sprintf("LogMP48Ws_%s.log", time.Now().Format("20060102"))
	auditPath := filepath.Join(config.AuditLogDir, filename)

	// Verifica se il file esiste
	info, err := os.Stat(auditPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("ℹ️ Audit log %s non esiste ancora, salto sincronizzazione", filename)
		} else {
			log.Printf("❌ Errore verifica audit log %s: %v", auditPath, err)
		}
		return
	}

	fileModTime := info.ModTime()

	// Controlla se il file è stato modificato dopo l'ultima sincronizzazione
	auditLogSyncMutex.Lock()
	lastSync, ok := auditLogLastSync[auditPath]
	if ok && !fileModTime.After(lastSync) {
		auditLogSyncMutex.Unlock()
		log.Printf("ℹ️ Audit log %s già sincronizzato (modifica: %s)", filename, fileModTime.Format("15:04:05"))
		return
	}
	auditLogSyncMutex.Unlock()

	// Sincronizza il file
	log.Printf("📤 Sincronizzazione audit log: %s (modificato %s)", filename, fileModTime.Format("15:04:05"))
	if err := SyncFileToAllRemotes(auditPath); err != nil {
		log.Printf("❌ Errore sincronizzazione audit log %s: %v", filename, err)
		return
	}

	// Aggiorna il timestamp di sincronizzazione
	auditLogSyncMutex.Lock()
	auditLogLastSync[auditPath] = fileModTime
	auditLogSyncMutex.Unlock()
	log.Printf("✅ Audit log sincronizzato: %s", filename)
}

// SyncAuditLogNowHandler sincronizza l'audit log su richiesta (endpoint manuale per admin)
func SyncAuditLogNowHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_, isAdmin := getUserContext(r)
	if !isAdmin {
		http.Error(w, "Accesso negato", http.StatusForbidden)
		return
	}

	// Esegue la sincronizzazione in background
	go syncAuditLog()

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Sincronizzazione audit log avviata in background"))
}
