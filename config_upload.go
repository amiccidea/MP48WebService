package main

import (
	"io"
	"log"
	"net/http"
	"os"
)

func configUploadHandler(w http.ResponseWriter, r *http.Request) {
	username, isAdmin := getUserContext(r)
	perms := getUserPermissions(username)
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
			Permissions:     perms,
		}
		tmpl.ExecuteTemplate(w, "layout.html", data)
		return
	}
	if r.Method == http.MethodPost {
		file, handler, err := r.FormFile("configfile")
		if err != nil {
			http.Error(w, "Errore nel file", http.StatusBadRequest)
			return
		}
		defer file.Close()
		os.MkdirAll(config.UploadDir, 0755)
		dstPath := config.UploadDir + "/" + handler.Filename
		dst, err := os.Create(dstPath)
		if err != nil {
			http.Error(w, "Errore salvataggio", http.StatusInternalServerError)
			return
		}
		defer dst.Close()
		io.Copy(dst, file)
		log.Printf("File %s caricato da %s", handler.Filename, username)
		WriteAuditLog("config_upload", username, "Configurazione caricata: "+handler.Filename)
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
			Message:         "File caricato con successo",
			Permissions:     perms,
		}
		tmpl.ExecuteTemplate(w, "layout.html", data)
		return
	}
}
