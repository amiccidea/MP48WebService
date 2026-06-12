package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"sync"
	"time"
)

// RebootOperation tiene traccia di un'operazione asincrona di riavvio in cascata
type RebootOperation struct {
	ID        string
	Status    string // pending, rebooting_remote, waiting_remote, rebooting_local, completed, error
	Message   string
	Error     string
	Timestamp time.Time
}

var (
	rebootOps   = make(map[string]*RebootOperation)
	rebootMutex sync.RWMutex
)

// ==================== FUNZIONI DI RIAVVIO ====================

// RebootLocal riavvia la macchina corrente (solo Linux)
func RebootLocal() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("reboot locale supportato solo su Linux")
	}
	return exec.Command("reboot").Run()
}

// RebootRemoteViaTelnet riavvia una macchina remota usando Telnet
func RebootRemoteViaTelnet(machine RemoteMachine) error {
	cfg := machine.Telnet
	if cfg.Host == "" {
		return fmt.Errorf("configurazione Telnet mancante per macchina %s", machine.ID)
	}
	address := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	conn, err := net.DialTimeout("tcp", address, time.Duration(cfg.TimeoutSec)*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(time.Duration(cfg.TimeoutSec) * time.Second))

	// Leggi eventuale banner (opzionale)
	conn.Write([]byte("\n"))
	time.Sleep(500 * time.Millisecond)

	// Invia username
	conn.Write([]byte(cfg.Username + "\n"))
	time.Sleep(500 * time.Millisecond)

	// Invia password
	conn.Write([]byte(cfg.Password + "\n"))
	time.Sleep(500 * time.Millisecond)

	// Invia comando reboot
	rebootCmd := cfg.RebootCommand
	if rebootCmd == "" {
		rebootCmd = "reboot"
	}
	conn.Write([]byte(rebootCmd + "\n"))
	return nil
}

// WaitForRemoteReachable attende che la macchina remota sia raggiungibile via TCP (porta telnet)
func WaitForRemoteReachable(machine RemoteMachine, maxWaitSec int) bool {
	address := fmt.Sprintf("%s:%d", machine.Telnet.Host, machine.Telnet.Port)
	timeout := time.After(time.Duration(maxWaitSec) * time.Second)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-timeout:
			return false
		case <-ticker.C:
			conn, err := net.DialTimeout("tcp", address, 3*time.Second)
			if err == nil {
				conn.Close()
				return true
			}
		}
	}
}

// startCascadeReboot avvia un riavvio in cascata per una macchina remota specifica
func startCascadeReboot(machine RemoteMachine) string {
	id := fmt.Sprintf("cascade_%s_%d", machine.ID, time.Now().UnixNano())
	op := &RebootOperation{
		ID:        id,
		Status:    "pending",
		Message:   "Avvio riavvio in cascata...",
		Timestamp: time.Now(),
	}
	rebootMutex.Lock()
	rebootOps[id] = op
	rebootMutex.Unlock()

	go func() {
		// 1. Riavvio remoto
		op.Status = "rebooting_remote"
		op.Message = "Riavvio macchina secondaria in corso..."
		if err := RebootRemoteViaTelnet(machine); err != nil {
			op.Status = "error"
			op.Error = err.Error()
			op.Message = "Errore riavvio remoto: " + err.Error()
			return
		}
		// 2. Attesa che il remoto sia raggiungibile
		op.Status = "waiting_remote"
		op.Message = "Attesa che la macchina secondaria si riavvii (max 2 minuti)..."
		if !WaitForRemoteReachable(machine, 120) {
			op.Status = "error"
			op.Error = "Timeout: la macchina remota non risponde dopo il riavvio"
			op.Message = "Errore attesa remoto"
			return
		}
		// 3. Riavvio locale
		op.Status = "rebooting_local"
		op.Message = "Riavvio macchina principale in corso..."
		if err := RebootLocal(); err != nil {
			op.Status = "error"
			op.Error = err.Error()
			op.Message = "Errore riavvio locale: " + err.Error()
			return
		}
		op.Status = "completed"
		op.Message = "Riavvio in cascata completato. La macchina principale si sta riavviando."
	}()
	return id
}

// ==================== HANDLER HTTP ====================

// Pagina principale del reboot
func rebootPageHandler(w http.ResponseWriter, r *http.Request) {
	username, isAdmin := getUserContext(r)
	if username == "" {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	if !isAdmin {
		http.Error(w, "Accesso negato", http.StatusForbidden)
		return
	}
	perms := getUserPermissions(username)
	data := struct {
		Username        string
		IsAdmin         bool
		Title           string
		ContentTemplate string
		Permissions     map[string]bool
		RemoteMachines  []RemoteMachine
	}{
		Username:        username,
		IsAdmin:         isAdmin,
		Title:           "Riavvio sistema",
		ContentTemplate: "rebootContent",
		Permissions:     perms,
		RemoteMachines:  config.RemoteMachines,
	}
	tmpl.ExecuteTemplate(w, "layout.html", data)
}

// Riavvio locale (solo POST)
func rebootLocalHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_, isAdmin := getUserContext(r)
	if !isAdmin {
		http.Error(w, "Accesso negato", http.StatusForbidden)
		return
	}
	go func() {
		if err := RebootLocal(); err != nil {
			log.Printf("Errore reboot locale: %v", err)
		}
	}()
	w.Write([]byte("Riavvio locale avviato"))
}

// Riavvio remoto per una specifica macchina (via query param "id")
func rebootRemoteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_, isAdmin := getUserContext(r)
	if !isAdmin {
		http.Error(w, "Accesso negato", http.StatusForbidden)
		return
	}
	machineID := r.URL.Query().Get("id")
	if machineID == "" {
		http.Error(w, "Missing machine id", http.StatusBadRequest)
		return
	}
	var machine RemoteMachine
	found := false
	for _, m := range config.RemoteMachines {
		if m.ID == machineID {
			machine = m
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "Macchina remota non trovata", http.StatusNotFound)
		return
	}
	if err := RebootRemoteViaTelnet(machine); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write([]byte(fmt.Sprintf("Riavvio remoto avviato per %s", machine.Name)))
}

// Riavvio in cascata per una specifica macchina (asincrono, restituisce operation ID)
func rebootCascadeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_, isAdmin := getUserContext(r)
	if !isAdmin {
		http.Error(w, "Accesso negato", http.StatusForbidden)
		return
	}
	machineID := r.URL.Query().Get("id")
	if machineID == "" {
		http.Error(w, "Missing machine id", http.StatusBadRequest)
		return
	}
	var machine RemoteMachine
	found := false
	for _, m := range config.RemoteMachines {
		if m.ID == machineID {
			machine = m
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "Macchina remota non trovata", http.StatusNotFound)
		return
	}
	opID := startCascadeReboot(machine)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"id": opID})
}

// Polling per lo stato dell'operazione di cascata
func rebootStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_, isAdmin := getUserContext(r)
	if !isAdmin {
		http.Error(w, "Accesso negato", http.StatusForbidden)
		return
	}
	opID := r.URL.Query().Get("id")
	if opID == "" {
		http.Error(w, "Missing id", http.StatusBadRequest)
		return
	}
	rebootMutex.RLock()
	op, ok := rebootOps[opID]
	rebootMutex.RUnlock()
	if !ok {
		http.Error(w, "Operation not found", http.StatusNotFound)
		return
	}
	resp := map[string]string{
		"status":  op.Status,
		"message": op.Message,
	}
	if op.Error != "" {
		resp["error"] = op.Error
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
