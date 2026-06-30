package main

import (
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"strconv"

	"github.com/gorilla/csrf"
)

func analogInputsPage(w http.ResponseWriter, r *http.Request) {
	username, isAdmin := getUserContext(r)
	if username == "" {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	if !hasPermission(r, PermAnalogInputs) {
		http.Error(w, "Accesso negato", http.StatusForbidden)
		return
	}
	perms := getUserPermissions(username)

	availableCPUs, err := GetAvailableCPUs()
	if err != nil {
		log.Printf("Errore recupero CPU disponibili: %v", err)
		availableCPUs = []int{1}
	}

	// CPU selezionata: default la prima disponibile
	cpuID := availableCPUs[0]
	if cpuParam := r.URL.Query().Get("cpu"); cpuParam != "" {
		if id, err := strconv.Atoi(cpuParam); err == nil {
			for _, c := range availableCPUs {
				if c == id {
					cpuID = id
					break
				}
			}
		}
	}

	data := struct {
		Username        string
		IsAdmin         bool
		Title           string
		ContentTemplate string
		Permissions     map[string]bool
		CPU             int
		AvailableCPUs   []int
		IsMultiCPU      bool
		CSRFField       template.HTML
		CSRFToken       string
	}{
		Username:        username,
		IsAdmin:         isAdmin,
		Title:           "Ingressi Analogici",
		ContentTemplate: "analogInputsContent",
		Permissions:     perms,
		CPU:             cpuID,
		AvailableCPUs:   availableCPUs,
		IsMultiCPU:      isMultiCPU(),
		CSRFField:       csrf.TemplateField(r),
		CSRFToken:       csrf.Token(r),
	}
	if err := tmpl.ExecuteTemplate(w, "layout.html", data); err != nil {
		log.Printf("❌ Errore rendering analog inputs: %v", err)
		http.Error(w, "Errore interno", http.StatusInternalServerError)
	}
}

func apiAnalogInputsHandler(w http.ResponseWriter, r *http.Request) {
	_, _ = getUserContext(r) // verifica autenticazione
	cpuParam := r.URL.Query().Get("cpu")
	cpuID := 1
	if cpuParam != "" {
		if id, err := strconv.Atoi(cpuParam); err == nil && id >= 1 && id <= 4 {
			cpuID = id
		}
	}
	inputs, err := GetAnalogInputs(cpuID)
	if err != nil {
		log.Printf("Errore recupero ingressi analogici: %v", err)
		http.Error(w, "Errore interno", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"inputs": inputs,
		"cpu":    cpuID,
	})
}
