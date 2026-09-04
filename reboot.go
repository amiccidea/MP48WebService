package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/csrf"
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

// RebootLocal riavvia la macchina corrente usando syscall.Reboot
func RebootLocal() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("reboot locale supportato solo su Linux")
	}
	Infof("Tentativo reboot locale con syscall.Reboot...")
	err := syscall.Reboot(syscall.LINUX_REBOOT_CMD_RESTART)
	if err != nil {
		Warnf("syscall.Reboot fallito: %v", err)
		Infof("Tentativo fallback con /sbin/reboot...")
		cmd := exec.Command("/sbin/reboot")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err2 := cmd.Run(); err2 != nil {
			return fmt.Errorf("syscall.Reboot: %v, /sbin/reboot: %v", err, err2)
		}
		return nil
	}
	Infof("syscall.Reboot eseguito con successo")
	return nil
}

// ================================================================
//  TELNET VIA SYSCALL
// ================================================================

// rebootViaSyscallTelnet usa la syscall per eseguire lo script wrapper Telnet
func rebootViaSyscallTelnet(machine *RemoteMachine) error {
	if machine.Host == "" {
		return fmt.Errorf("IP non risolto per macchina %s", machine.ID)
	}

	username := machine.TelnetUsername
	password := machine.TelnetPassword
	sudoPwd := machine.SudoPassword

	// Ricarica credenziali se mancano
	if username == "" || password == "" {
		Warnf("Credenziali mancanti per %s (Telnet), tentativo di ricarica...", machine.ID)
		remoteCreds, err := loadRemoteCredentials(currentDataDir)
		if err != nil {
			return fmt.Errorf("errore caricamento credenziali: %w", err)
		}
		if remoteCreds != nil && remoteCreds.Machines != nil {
			if cred, ok := remoteCreds.Machines[machine.ID]; ok {
				machine.TelnetUsername = cred.TelnetUsername
				machine.TelnetPassword = cred.TelnetPassword
				machine.SudoPassword = cred.SudoPassword
				username = cred.TelnetUsername
				password = cred.TelnetPassword
				sudoPwd = cred.SudoPassword
				Infof("Credenziali ricaricate per %s (Telnet): username='%s'", machine.ID, username)
			} else {
				Warnf("Nessuna credenziale per %s in remote_creds.enc", machine.ID)
			}
		}
	}

	if username == "" || password == "" {
		return fmt.Errorf("credenziali non configurate per %s. Vai su /admin/remote-credentials per configurarle", machine.Name)
	}

	scriptPath := machine.Telnet.ScriptPath
	if scriptPath == "" {
		scriptPath = "/usr/local/bin/reboot_remote.sh"
		Warnf("ScriptPath Telnet non configurato per %s, uso fallback: %s", machine.Name, scriptPath)
	}

	rebootCmd := machine.Telnet.RebootCommand
	useSudo := strings.Contains(rebootCmd, "sudo")

	argv := []string{
		scriptPath,
		machine.Host,
		username,
		password,
		sudoPwd,
		fmt.Sprintf("%t", useSudo),
	}

	envv := []string{
		"PATH=/usr/local/bin:/usr/bin:/bin:/sbin",
		"HOME=/root",
	}

	Infof("[TELNET] Eseguo script: %s su %s", scriptPath, machine.Host)

	pid, err := InvocatoreSyscallNativo(scriptPath, argv, envv)
	if err != nil {
		return fmt.Errorf("syscall fallita: %w", err)
	}

	var wstatus syscall.WaitStatus
	_, err = syscall.Wait4(pid, &wstatus, 0, nil)
	if err != nil {
		return fmt.Errorf("errore wait4: %w", err)
	}

	if wstatus.ExitStatus() != 0 {
		return fmt.Errorf("script terminato con codice %d", wstatus.ExitStatus())
	}

	statusFile := "/tmp/reboot_status.txt"
	data, err := ioutil.ReadFile(statusFile)
	if err != nil {
		Warnf("Impossibile leggere %s: %v", statusFile, err)
		return nil
	}
	status := strings.TrimSpace(string(data))
	if status == "REBOOT_FAILED" {
		return fmt.Errorf("reboot fallito (stato da file)")
	}

	Infof("[TELNET] Reboot remoto completato per %s", machine.Name)
	return nil
}

// ================================================================
//  SSH VIA SYSCALL (wrapper script)
// ================================================================

// rebootViaSyscallSSH usa la syscall per eseguire lo script wrapper SSH
func rebootViaSyscallSSH(machine *RemoteMachine) error {
	if machine.Host == "" {
		return fmt.Errorf("IP non risolto per macchina %s", machine.ID)
	}

	username := machine.TelnetUsername
	password := machine.TelnetPassword
	sudoPwd := machine.SudoPassword

	// Ricarica credenziali se mancano
	if username == "" || password == "" {
		Warnf("Credenziali mancanti per %s (SSH), tentativo di ricarica...", machine.ID)
		remoteCreds, err := loadRemoteCredentials(currentDataDir)
		if err != nil {
			return fmt.Errorf("errore caricamento credenziali: %w", err)
		}
		if remoteCreds != nil && remoteCreds.Machines != nil {
			if cred, ok := remoteCreds.Machines[machine.ID]; ok {
				machine.TelnetUsername = cred.TelnetUsername
				machine.TelnetPassword = cred.TelnetPassword
				machine.SudoPassword = cred.SudoPassword
				username = cred.TelnetUsername
				password = cred.TelnetPassword
				sudoPwd = cred.SudoPassword
				Infof("Credenziali ricaricate per %s (SSH): username='%s'", machine.ID, username)
			} else {
				Warnf("Nessuna credenziale per %s in remote_creds.enc", machine.ID)
			}
		}
	}

	if username == "" || password == "" {
		return fmt.Errorf("credenziali non configurate per %s. Vai su /admin/remote-credentials per configurarle", machine.Name)
	}

	scriptPath := machine.SSH.ScriptPath
	if scriptPath == "" {
		scriptPath = "/usr/local/bin/reboot_ssh.sh"
		Warnf("ScriptPath SSH non configurato per %s, uso fallback: %s", machine.Name, scriptPath)
	}

	rebootCmd := machine.SSH.RebootCommand
	if rebootCmd == "" {
		rebootCmd = "sudo reboot"
	}

	argv := []string{
		scriptPath,
		machine.Host,
		username,
		password,
		sudoPwd,
		rebootCmd,
	}

	envv := []string{
		"PATH=/usr/local/bin:/usr/bin:/bin:/sbin",
		"HOME=/root",
	}

	Infof("[SSH] Eseguo script: %s su %s", scriptPath, machine.Host)

	pid, err := InvocatoreSyscallNativo(scriptPath, argv, envv)
	if err != nil {
		return fmt.Errorf("syscall fallita: %w", err)
	}

	var wstatus syscall.WaitStatus
	_, err = syscall.Wait4(pid, &wstatus, 0, nil)
	if err != nil {
		return fmt.Errorf("errore wait4: %w", err)
	}

	if wstatus.ExitStatus() != 0 {
		return fmt.Errorf("script terminato con codice %d", wstatus.ExitStatus())
	}

	statusFile := "/tmp/reboot_status.txt"
	data, err := ioutil.ReadFile(statusFile)
	if err != nil {
		Warnf("Impossibile leggere %s: %v", statusFile, err)
		return nil
	}
	status := strings.TrimSpace(string(data))
	if status == "REBOOT_FAILED" {
		return fmt.Errorf("reboot fallito (stato da file)")
	}

	Infof("[SSH] Reboot remoto completato per %s", machine.Name)
	return nil
}

// ================================================================
//  DISPATCHER
// ================================================================

// rebootRemote dispatcher: sceglie il metodo appropriato in base a UseSSH
func rebootRemote(machine *RemoteMachine) error {
	if machine.UseSSH {
		return rebootViaSyscallSSH(machine)
	}
	return rebootViaSyscallTelnet(machine)
}

// WaitForRemoteReachable attende che la macchina remota sia raggiungibile
func WaitForRemoteReachable(machine *RemoteMachine, maxWaitSec int, opID string) bool {
	if machine.Host == "" {
		updateRebootStatus(opID, "error", "IP non risolto per la macchina remota")
		return false
	}

	var port int
	if machine.UseSSH {
		port = machine.SSH.Port
		if port == 0 {
			port = 22
		}
	} else {
		port = machine.Telnet.Port
		if port == 0 {
			port = 23
		}
	}
	address := fmt.Sprintf("%s:%d", machine.Host, port)

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

// ==================== START RIAVVIO ====================

// startRemoteReboot esegue il reboot remoto (SSH o Telnet)
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
		method := "TELNET"
		if machine.UseSSH {
			method = "SSH"
		}
		updateRebootStatus(id, "rebooting_remote", fmt.Sprintf("🔄 Riavvio %s (%s) via %s...", machine.Name, machine.Host, method))

		if err := rebootRemote(machine); err != nil {
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

// startCascadeReboot esegue reboot remoto, attende e poi riavvia locale
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
		method := "TELNET"
		if machine.UseSSH {
			method = "SSH"
		}
		updateRebootStatus(id, "rebooting_remote", fmt.Sprintf("🔄 Riavvio %s (%s) via %s...", machine.Name, machine.Host, method))

		if err := rebootRemote(machine); err != nil {
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
		for _, machine := range config.RemoteMachines {
			if machine.ID == "local" {
				continue
			}
			method := "TELNET"
			if machine.UseSSH {
				method = "SSH"
			}
			updateRebootStatus(id, "rebooting_remote", fmt.Sprintf("🔄 Riavvio %s (%s) via %s...", machine.Name, machine.Host, method))

			if err := rebootRemote(&machine); err != nil {
				updateRebootStatus(id, "error", fmt.Sprintf("Errore riavvio remoto %s: %v", machine.Name, err))
				return
			}

			updateRebootStatus(id, "waiting_remote", fmt.Sprintf("Attesa che %s (%s) si riavvii (max 2 minuti)...", machine.Name, machine.Host))
			if !WaitForRemoteReachable(&machine, 120, id) {
				return
			}
			updateRebootStatus(id, "waiting_remote", fmt.Sprintf("✅ %s (%s) è raggiungibile!", machine.Name, machine.Host))
		}
		updateRebootStatus(id, "rebooting_local", "Riavvio macchina principale in corso...")
		if err := RebootLocal(); err != nil {
			updateRebootStatus(id, "error", fmt.Sprintf("Errore riavvio locale: %v", err))
			return
		}
		updateRebootStatus(id, "completed", "Riavvio in cascata completato. Tutte le macchine sono state riavviate.")
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
		CSRFField       template.HTML
		CSRFToken       string
		MFAEnabled      bool
	}{
		Username:        username,
		IsAdmin:         isAdmin,
		Title:           "Riavvio sistema",
		ContentTemplate: "rebootContent",
		Permissions:     perms,
		RemoteMachines:  config.RemoteMachines,
		IsMultiCPU:      isMultiCPU(),
		CSRFField:       csrf.TemplateField(r),
		CSRFToken:       csrf.Token(r),
		MFAEnabled:      config.MFAEnabled,
	}
	if err := tmpl.ExecuteTemplate(w, "layout.html", data); err != nil {
		Errorf("Errore rendering reboot page: %v", err)
		http.Error(w, "Errore interno", http.StatusInternalServerError)
	}
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
	username, _ := getUserContext(r)
	go RebootLocal()
	WriteAuditLogWithRequest("REBOOT_LOCAL", username, "Riavvio locale avviato", r)
	w.WriteHeader(http.StatusOK)
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