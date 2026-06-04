package main

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

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
	data := struct {
		Username        string
		IsAdmin         bool
		Title           string
		ContentTemplate string
		Files           []FileInfo
		StartDate       string
		EndDate         string
		Extensions      []string
		Permissions     map[string]bool
	}{
		Username:        username,
		IsAdmin:         isAdmin,
		Title:           "Storico Configurazioni",
		ContentTemplate: "configHistoryContent",
		Files:           files,
		StartDate:       startDate,
		EndDate:         endDate,
		Extensions:      extensions,
		Permissions:     perms,
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
