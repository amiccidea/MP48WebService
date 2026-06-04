package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
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
	}{
		Username:        username,
		IsAdmin:         isAdmin,
		Title:           "Logs",
		ContentTemplate: "logsContent",
		Permissions:     perms,
	}
	tmpl.ExecuteTemplate(w, "layout.html", data)
}

func scanAllLogs() ([]LogFileInfo, error) {
	var allLogs []LogFileInfo
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
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"logs":       paginatedLogs,
		"categories": getCategoriesList(),
		"total":      total,
		"page":       page,
		"pageSize":   pageSize,
		"totalPages": totalPages,
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
