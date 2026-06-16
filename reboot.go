package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

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

func RebootLocal() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("reboot locale supportato solo su Linux")
	}
	return exec.Command("reboot").Run()
}

// RebootRemoteViaTelnet invia il comando reboot via Telnet (client esterno)
func RebootRemoteViaTelnet(machine RemoteMachine) error {
	cfg := machine.Telnet
	if cfg.Host == "" {
		return fmt.Errorf("configurazione Telnet mancante per macchina %s", machine.ID)
	}
	commands := []string{cfg.Username, cfg.Password}
	rebootCmd := cfg.RebootCommand
	if rebootCmd == "" {
		rebootCmd = "reboot"
	}
	if strings.Contains(rebootCmd, "sudo") && cfg.SudoPassword != "" {
		commands = append(commands, rebootCmd, cfg.SudoPassword)
	} else {
		commands = append(commands, rebootCmd)
	}
	cmd := exec.Command("telnet", cfg.Host, fmt.Sprintf("%d", cfg.Port))
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	for _, c := range commands {
		time.Sleep(800 * time.Millisecond)
		if _, err := stdin.Write([]byte(c + "\n")); err != nil {
			return err
		}
	}
	time.Sleep(2 * time.Second)
	stdin.Close()
	log.Printf("Comando reboot inviato a %s (%s)", machine.Name, machine.ID)
	return nil
}

// WaitForRemoteReachable attende che la macchina remota sia raggiungibile (tentativi ogni 30 sec)
func WaitForRemoteReachable(machine RemoteMachine, maxWaitSec int, opID string) bool {
	address := fmt.Sprintf("%s:%d", machine.Telnet.Host, machine.Telnet.Port)
	timeout := time.After(time.Duration(maxWaitSec) * time.Second)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	attempt := 1
	maxAttempts := (maxWaitSec / 30) + 1

	for {
		select {
		case <-timeout:
			updateRebootStatus(opID, "error", "Timeout: la macchina remota non risponde dopo il riavvio")
			return false
		case <-ticker.C:
			msg := fmt.Sprintf("Tentativo %d/%d: controllo raggiungibilità di %s...", attempt, maxAttempts, machine.Name)
			updateRebootStatus(opID, "waiting_remote", msg)

			conn, err := net.DialTimeout("tcp", address, 5*time.Second)
			if err == nil {
				conn.Close()
				updateRebootStatus(opID, "waiting_remote", fmt.Sprintf("✅ %s è raggiungibile!", machine.Name))
				return true
			}
			attempt++
		}
	}
}

// startRemoteReboot esegue il reboot remoto e attende la raggiungibilità (senza reboot locale)
func startRemoteReboot(machine RemoteMachine) string {
	id := fmt.Sprintf("remote_%s_%d", machine.ID, time.Now().UnixNano())
	op := &RebootOperation{
		ID:        id,
		Status:    "pending",
		Message:   "Avvio riavvio remoto...",
		Timestamp: time.Now(),
	}
	rebootMutex.Lock()
	rebootOps[id] = op
	rebootMutex.Unlock()

	go func() {
		updateRebootStatus(id, "rebooting_remote", fmt.Sprintf("Riavvio %s in corso...", machine.Name))
		if err := RebootRemoteViaTelnet(machine); err != nil {
			updateRebootStatus(id, "error", fmt.Sprintf("Errore riavvio remoto: %v", err))
			return
		}
		updateRebootStatus(id, "waiting_remote", fmt.Sprintf("Attesa che %s si riavvii (max 2 minuti)...", machine.Name))
		if !WaitForRemoteReachable(machine, 120, id) {
			return
		}
		updateRebootStatus(id, "completed", fmt.Sprintf("✅ %s è tornato online!", machine.Name))
	}()
	return id
}

// startCascadeReboot esegue reboot remoto, attende raggiungibilità e poi riavvia locale
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
		updateRebootStatus(id, "rebooting_remote", fmt.Sprintf("Riavvio %s in corso...", machine.Name))
		if err := RebootRemoteViaTelnet(machine); err != nil {
			updateRebootStatus(id, "error", fmt.Sprintf("Errore riavvio remoto: %v", err))
			return
		}
		updateRebootStatus(id, "waiting_remote", fmt.Sprintf("Attesa che %s si riavvii (max 2 minuti)...", machine.Name))
		if !WaitForRemoteReachable(machine, 120, id) {
			return
		}
		updateRebootStatus(id, "rebooting_local", "Riavvio macchina principale in corso...")
		if err := RebootLocal(); err != nil {
			updateRebootStatus(id, "error", fmt.Sprintf("Errore riavvio locale: %v", err))
			return
		}
		updateRebootStatus(id, "completed", "Riavvio in cascata completato. La macchina principale si sta riavviando.")
	}()
	return id
}

func updateRebootStatus(opID, status, message string) {
	rebootMutex.Lock()
	defer rebootMutex.Unlock()
	if op, ok := rebootOps[opID]; ok {
		op.Status = status
		op.Message = message
		if status == "error" {
			op.Error = message
		}
	}
}

// ==================== HANDLER HTTP ====================

func rebootPageHandler(w http.ResponseWriter, r *http.Request) {
	username, isAdmin := getUserContext(r)
	if username == "" || !isAdmin {
		http.Redirect(w, r, "/login", http.StatusFound)
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
	go RebootLocal()
	w.Write([]byte("Riavvio locale avviato"))
}

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
	machine, found := findRemoteMachine(machineID)
	if !found {
		http.Error(w, "Macchina remota non trovata", http.StatusNotFound)
		return
	}
	opID := startRemoteReboot(machine)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"id": opID})
}

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
	machine, found := findRemoteMachine(machineID)
	if !found {
		http.Error(w, "Macchina remota non trovata", http.StatusNotFound)
		return
	}
	opID := startCascadeReboot(machine)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"id": opID})
}

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

func findRemoteMachine(id string) (RemoteMachine, bool) {
	for _, m := range config.RemoteMachines {
		if m.ID == id {
			return m, true
		}
	}
	return RemoteMachine{}, false
}
