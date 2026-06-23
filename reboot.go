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

// RebootLocal riavvia la macchina corrente (solo Linux)
func RebootLocal() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("reboot locale supportato solo su Linux")
	}
	return exec.Command("reboot").Run()
}

// RebootRemoteViaTelnet invia il comando reboot via Telnet (client esterno)
func RebootRemoteViaTelnet(machine *RemoteMachine) error {
	if machine.Host == "" {
		return fmt.Errorf("IP non risolto per macchina %s", machine.ID)
	}

	// Ottieni i parametri
	username := machine.TelnetUsername
	password := machine.TelnetPassword
	sudoPwd := machine.SudoPassword
	port := machine.Telnet.Port
	if port == 0 {
		port = 23
	}
	rebootCmd := machine.Telnet.RebootCommand
	if rebootCmd == "" {
		rebootCmd = "reboot"
	}

	// Se mancano le credenziali, prova a ricaricarle da remote_creds.enc
	if username == "" || password == "" {
		log.Printf("Credenziali mancanti per %s, tentativo di ricarica...", machine.ID)
		remoteCreds, err := loadRemoteCredentials(currentDataDir)
		if err != nil {
			return fmt.Errorf("errore caricamento credenziali: %w", err)
		}
		if remoteCreds != nil && remoteCreds.Machines != nil {
			if cred, ok := remoteCreds.Machines[machine.ID]; ok {
				// Aggiorna la configurazione globale
				for i := range config.RemoteMachines {
					if config.RemoteMachines[i].ID == machine.ID {
						config.RemoteMachines[i].TelnetUsername = cred.TelnetUsername
						config.RemoteMachines[i].TelnetPassword = cred.TelnetPassword
						config.RemoteMachines[i].SudoPassword = cred.SudoPassword
						// Aggiorna anche la copia locale
						machine.TelnetUsername = cred.TelnetUsername
						machine.TelnetPassword = cred.TelnetPassword
						machine.SudoPassword = cred.SudoPassword
						username = cred.TelnetUsername
						password = cred.TelnetPassword
						sudoPwd = cred.SudoPassword
						break
					}
				}
			}
		}
		if username == "" || password == "" {
			return fmt.Errorf("credenziali Telnet non configurate per %s. Vai su /admin/remote-credentials per configurarle", machine.Name)
		}
	}

	// Prepara i comandi da inviare via telnet
	commands := []string{username, password}

	// Gestione sudo con password
	if strings.Contains(rebootCmd, "sudo") && sudoPwd != "" {
		// Usa echo + sudo -S per passare la password in modo non interattivo
		fullCmd := fmt.Sprintf("echo '%s' | sudo -S %s", sudoPwd, rebootCmd)
		commands = append(commands, fullCmd)
	} else {
		commands = append(commands, rebootCmd)
	}

	// Esegui telnet esterno
	cmd := exec.Command("telnet", machine.Host, fmt.Sprintf("%d", port))
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}

	// Avvia il comando
	if err := cmd.Start(); err != nil {
		return err
	}

	// Invia i comandi con pause
	for _, c := range commands {
		time.Sleep(800 * time.Millisecond)
		if _, err := stdin.Write([]byte(c + "\n")); err != nil {
			return err
		}
	}

	// Aspetta che il comando venga elaborato
	time.Sleep(2 * time.Second)
	stdin.Close()

	log.Printf("Comando reboot inviato a %s (%s)", machine.Name, machine.Host)
	return nil
}

// WaitForRemoteReachable attende che la macchina remota sia raggiungibile (tentativi ogni 30 sec)
func WaitForRemoteReachable(machine *RemoteMachine, maxWaitSec int, opID string) bool {
	if machine.Host == "" {
		updateRebootStatus(opID, "error", "IP non risolto per la macchina remota")
		return false
	}
	address := fmt.Sprintf("%s:%d", machine.Host, machine.Telnet.Port)
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
			msg := fmt.Sprintf("Tentativo %d/%d: controllo raggiungibilità di %s (%s)...", attempt, maxAttempts, machine.Name, machine.Host)
			updateRebootStatus(opID, "waiting_remote", msg)

			conn, err := net.DialTimeout("tcp", address, 5*time.Second)
			if err == nil {
				conn.Close()
				updateRebootStatus(opID, "waiting_remote", fmt.Sprintf("✅ %s (%s) è raggiungibile!", machine.Name, machine.Host))
				return true
			}
			attempt++
		}
	}
}

// startRemoteReboot esegue il reboot remoto e attende la raggiungibilità (senza reboot locale)
func startRemoteReboot(machine *RemoteMachine) string {
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
		updateRebootStatus(id, "rebooting_remote", fmt.Sprintf("Riavvio %s (%s) in corso...", machine.Name, machine.Host))
		if err := RebootRemoteViaTelnet(machine); err != nil {
			updateRebootStatus(id, "error", fmt.Sprintf("Errore riavvio remoto: %v", err))
			return
		}
		updateRebootStatus(id, "waiting_remote", fmt.Sprintf("Attesa che %s (%s) si riavvii (max 2 minuti)...", machine.Name, machine.Host))
		if !WaitForRemoteReachable(machine, 120, id) {
			return
		}
		updateRebootStatus(id, "completed", fmt.Sprintf("✅ %s (%s) è tornato online!", machine.Name, machine.Host))
	}()
	return id
}

// startCascadeReboot esegue reboot remoto, attende raggiungibilità e poi riavvia locale
func startCascadeReboot(machine *RemoteMachine) string {
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
		updateRebootStatus(id, "rebooting_remote", fmt.Sprintf("Riavvio %s (%s) in corso...", machine.Name, machine.Host))
		if err := RebootRemoteViaTelnet(machine); err != nil {
			updateRebootStatus(id, "error", fmt.Sprintf("Errore riavvio remoto: %v", err))
			return
		}
		updateRebootStatus(id, "waiting_remote", fmt.Sprintf("Attesa che %s (%s) si riavvii (max 2 minuti)...", machine.Name, machine.Host))
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

// updateRebootStatus aggiorna lo stato di un'operazione (thread-safe)
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
		IsMultiCPU      bool
	}{
		Username:        username,
		IsAdmin:         isAdmin,
		Title:           "Riavvio sistema",
		ContentTemplate: "rebootContent",
		Permissions:     perms,
		RemoteMachines:  config.RemoteMachines,
		IsMultiCPU:      isMultiCPU(),
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
	if machineID == "" {
		http.Error(w, "Missing machine id", http.StatusBadRequest)
		return
	}
	machine, found := findRemoteMachine(machineID)
	if !found {
		http.Error(w, "Macchina remota non trovata", http.StatusNotFound)
		return
	}
	if machine.Host == "" {
		http.Error(w, "IP non risolto per questa macchina", http.StatusInternalServerError)
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
	if machineID == "" {
		http.Error(w, "Missing machine id", http.StatusBadRequest)
		return
	}
	machine, found := findRemoteMachine(machineID)
	if !found {
		http.Error(w, "Macchina remota non trovata", http.StatusNotFound)
		return
	}
	if machine.Host == "" {
		http.Error(w, "IP non risolto per questa macchina", http.StatusInternalServerError)
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

// findRemoteMachine restituisce un puntatore alla macchina remota e un bool
func findRemoteMachine(id string) (*RemoteMachine, bool) {
	for i := range config.RemoteMachines {
		if config.RemoteMachines[i].ID == id {
			return &config.RemoteMachines[i], true
		}
	}
	return nil, false
}

// rebootCascadeAllHandler avvia il riavvio in cascata di TUTTE le macchine remote
func rebootCascadeAllHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_, isAdmin := getUserContext(r)
	if !isAdmin {
		http.Error(w, "Accesso negato", http.StatusForbidden)
		return
	}
	opID := startCascadeAllReboot()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"id": opID})
}

// startCascadeAllReboot avvia il riavvio in cascata di TUTTE le macchine remote
func startCascadeAllReboot() string {
	id := fmt.Sprintf("cascade_all_%d", time.Now().UnixNano())
	op := &RebootOperation{
		ID:        id,
		Status:    "pending",
		Message:   "Avvio riavvio in cascata di tutte le macchine...",
		Timestamp: time.Now(),
	}
	rebootMutex.Lock()
	rebootOps[id] = op
	rebootMutex.Unlock()

	go func() {
		// 1. Riavvia tutte le macchine remote (esclusa "local")
		for _, machine := range config.RemoteMachines {
			if machine.ID == "local" {
				continue
			}
			// Riavvio remoto
			updateRebootStatus(id, "rebooting_remote", fmt.Sprintf("Riavvio %s (%s) in corso...", machine.Name, machine.Host))
			// Passiamo una copia per evitare problemi di concorrenza
			machineCopy := machine
			if err := RebootRemoteViaTelnet(&machineCopy); err != nil {
				updateRebootStatus(id, "error", fmt.Sprintf("Errore riavvio remoto %s: %v", machine.Name, err))
				return
			}
			// Attendi che il remoto sia raggiungibile
			updateRebootStatus(id, "waiting_remote", fmt.Sprintf("Attesa che %s (%s) si riavvii (max 2 minuti)...", machine.Name, machine.Host))
			if !WaitForRemoteReachable(&machineCopy, 120, id) {
				updateRebootStatus(id, "error", fmt.Sprintf("Timeout: %s non risponde dopo il riavvio", machine.Name))
				return
			}
			updateRebootStatus(id, "waiting_remote", fmt.Sprintf("✅ %s (%s) è raggiungibile!", machine.Name, machine.Host))
		}
		// 2. Riavvio locale
		updateRebootStatus(id, "rebooting_local", "Riavvio macchina principale in corso...")
		if err := RebootLocal(); err != nil {
			updateRebootStatus(id, "error", fmt.Sprintf("Errore riavvio locale: %v", err))
			return
		}
		updateRebootStatus(id, "completed", "Riavvio in cascata completato. Tutte le macchine sono state riavviate.")
	}()
	return id
}
