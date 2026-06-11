package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
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

// Strutture per i dati
type CurrentFileInfo struct {
	Name    string
	ModTime string
	Size    string
	Path    string
}

type BackupFileInfo struct {
	Name       string
	ModTime    string
	ModTimeRaw time.Time
	Size       string
}

// Helper: crea backup dell'intera directory corrente (zip)
func backupCurrentConfigDir() (string, string, error) {
	if config.CurrentConfigurationDir == "" {
		return "", "", fmt.Errorf("directory corrente non configurata")
	}
	if err := os.MkdirAll(config.ConfigHistoryDir, 0755); err != nil {
		return "", "", err
	}
	timestamp := time.Now().Format("20060102_150405")
	backupName := fmt.Sprintf("config_backup_%s.zip", timestamp)
	backupPath := filepath.Join(config.ConfigHistoryDir, backupName)

	zipFile, err := os.Create(backupPath)
	if err != nil {
		return "", "", err
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	err = filepath.Walk(config.CurrentConfigurationDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == config.CurrentConfigurationDir {
			return nil
		}
		relPath, err := filepath.Rel(config.CurrentConfigurationDir, path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			_, err = zipWriter.Create(relPath + "/")
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = relPath
		header.Method = zip.Deflate
		writer, err := zipWriter.CreateHeader(header)
		if err != nil {
			return err
		}
		_, err = io.Copy(writer, file)
		return err
	})
	if err != nil {
		return "", "", err
	}
	return backupPath, backupName, nil
}

// Estrazione archivi (utile per ripristino)
func extractArchive(archivePath, destDir string) error {
	ext := strings.ToLower(filepath.Ext(archivePath))
	switch ext {
	case ".zip":
		return extractZip(archivePath, destDir)
	case ".tar":
		return extractTar(archivePath, destDir)
	case ".gz":
		if strings.HasSuffix(strings.ToLower(archivePath), ".tar.gz") || strings.HasSuffix(strings.ToLower(archivePath), ".tgz") {
			return extractTarGz(archivePath, destDir)
		}
		return fmt.Errorf("formato non supportato: %s", ext)
	default:
		return fmt.Errorf("formato non supportato: %s", ext)
	}
}

func extractZip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		fpath := filepath.Join(destDir, f.Name)
		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}
		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}
		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func extractTar(tarPath, destDir string) error {
	f, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer f.Close()
	tr := tar.NewReader(f)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		target := filepath.Join(destDir, header.Name)
		switch header.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(target, os.FileMode(header.Mode))
		case tar.TypeReg:
			os.MkdirAll(filepath.Dir(target), os.ModePerm)
			outFile, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return err
			}
			outFile.Close()
		}
	}
	return nil
}

func extractTarGz(tarGzPath, destDir string) error {
	f, err := os.Open(tarGzPath)
	if err != nil {
		return err
	}
	defer f.Close()
	gzr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gzr.Close()
	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		target := filepath.Join(destDir, header.Name)
		switch header.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(target, os.FileMode(header.Mode))
		case tar.TypeReg:
			os.MkdirAll(filepath.Dir(target), os.ModePerm)
			outFile, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return err
			}
			outFile.Close()
		}
	}
	return nil
}

// Handler principale storico configurazioni
func configHistoryHandler(w http.ResponseWriter, r *http.Request) {
	username, isAdmin := getUserContext(r)
	perms := getUserPermissions(username)

	backupDir := config.ConfigHistoryDir
	if backupDir == "" {
		backupDir = "./config_history"
	}
	backupExtensions := []string{".zip", ".tar", ".tar.gz", ".tgz"}
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
	var backups []BackupFileInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		extOk := false
		for _, ext := range backupExtensions {
			if strings.HasSuffix(strings.ToLower(name), ext) {
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
		backups = append(backups, BackupFileInfo{
			Name:       name,
			ModTime:    modTime.Format("2006-01-02 15:04:05"),
			ModTimeRaw: modTime,
			Size:       formatFileSize(info.Size()),
		})
	}
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].ModTimeRaw.After(backups[j].ModTimeRaw)
	})

	var currentFiles []CurrentFileInfo
	if config.CurrentConfigurationDir != "" {
		err := filepath.Walk(config.CurrentConfigurationDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			relPath, err := filepath.Rel(config.CurrentConfigurationDir, path)
			if err != nil {
				relPath = info.Name()
			}
			currentFiles = append(currentFiles, CurrentFileInfo{
				Name:    info.Name(),
				ModTime: info.ModTime().Format("2006-01-02 15:04:05"),
				Size:    formatFileSize(info.Size()),
				Path:    relPath,
			})
			return nil
		})
		if err != nil {
			log.Printf("Errore walking config dir: %v", err)
		}
		sort.Slice(currentFiles, func(i, j int) bool {
			return currentFiles[i].Name < currentFiles[j].Name
		})
	}

	data := struct {
		Username        string
		IsAdmin         bool
		Title           string
		ContentTemplate string
		Backups         []BackupFileInfo
		CurrentFiles    []CurrentFileInfo
		StartDate       string
		EndDate         string
		Permissions     map[string]bool
	}{
		Username:        username,
		IsAdmin:         isAdmin,
		Title:           "Storico Configurazioni",
		ContentTemplate: "configHistoryContent",
		Backups:         backups,
		CurrentFiles:    currentFiles,
		StartDate:       startDate,
		EndDate:         endDate,
		Permissions:     perms,
	}
	tmpl.ExecuteTemplate(w, "layout.html", data)
}

// Download backup
func configHistoryDownloadHandler(w http.ResponseWriter, r *http.Request) {
	filename := strings.TrimPrefix(r.URL.Path, "/config-history/download/")
	if filename == "" {
		http.Error(w, "Nome file mancante", http.StatusBadRequest)
		return
	}
	filePath := filepath.Join(config.ConfigHistoryDir, filename)
	info, err := os.Stat(filePath)
	if err != nil {
		http.Error(w, "File non trovato", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	http.ServeFile(w, r, filePath)
}

// Download singolo file configurazione corrente
func configCurrentFileDownloadHandler(w http.ResponseWriter, r *http.Request) {
	filePathParam := r.URL.Query().Get("path")
	if filePathParam == "" {
		http.Error(w, "Percorso mancante", http.StatusBadRequest)
		return
	}
	cleanPath := filepath.Clean(filePathParam)
	if strings.Contains(cleanPath, "..") {
		http.Error(w, "Percorso non valido", http.StatusBadRequest)
		return
	}
	fullPath := filepath.Join(config.CurrentConfigurationDir, cleanPath)
	info, err := os.Stat(fullPath)
	if err != nil {
		http.Error(w, "File non trovato", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename=\""+info.Name()+"\"")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	http.ServeFile(w, r, fullPath)
}

// Elimina backup
func configHistoryDeleteHandler(w http.ResponseWriter, r *http.Request) {
	filename := strings.TrimPrefix(r.URL.Path, "/config-history/delete/")
	if filename == "" {
		http.Error(w, "Nome file mancante", http.StatusBadRequest)
		return
	}
	if strings.Contains(filename, "..") {
		http.Error(w, "Percorso non valido", http.StatusBadRequest)
		return
	}
	filePath := filepath.Join(config.ConfigHistoryDir, filename)
	if err := os.Remove(filePath); err != nil {
		http.Error(w, "Errore eliminazione", http.StatusInternalServerError)
		return
	}
	username, _ := getUserContext(r)
	WriteAuditLog("CONFIG_DELETE", username, filename)
	w.WriteHeader(http.StatusOK)
}

// Ripristina backup (sovrascrive, non cancella)
func configHistoryRestoreHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_, isAdmin := getUserContext(r)
	if !isAdmin {
		http.Error(w, "Accesso negato", http.StatusForbidden)
		return
	}
	filename := strings.TrimPrefix(r.URL.Path, "/config-history/restore/")
	if filename == "" {
		http.Error(w, "Nome file mancante", http.StatusBadRequest)
		return
	}
	backupPath := filepath.Join(config.ConfigHistoryDir, filename)
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		http.Error(w, "File di backup non trovato", http.StatusNotFound)
		return
	}
	if config.CurrentConfigurationDir == "" {
		http.Error(w, "Directory corrente non configurata", http.StatusInternalServerError)
		return
	}
	// Backup automatico
	_, backupZipName, err := backupCurrentConfigDir()
	if err != nil {
		log.Printf("Errore backup: %v", err)
		http.Error(w, "Errore backup", http.StatusInternalServerError)
		return
	}
	log.Printf("Backup automatico creato: %s", backupZipName)
	// Estrai archivio (sovrascrive, non cancella)
	if err := extractArchive(backupPath, config.CurrentConfigurationDir); err != nil {
		log.Printf("Errore estrazione: %v", err)
		http.Error(w, "Errore durante il ripristino", http.StatusInternalServerError)
		return
	}
	username, _ := getUserContext(r)
	WriteAuditLog("CONFIG_RESTORE", username, fmt.Sprintf("ripristinato backup %s (backup automatico: %s)", filename, backupZipName))
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}
