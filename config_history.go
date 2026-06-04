package main

import (
	"net/http"
	"os"
	"sort"
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
