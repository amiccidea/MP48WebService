package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
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
	log.Printf("🔄 Tentativo reboot locale con syscall.Reboot...")
	err := syscall.Reboot(syscall.LINUX_REBOOT_CMD_RESTART)
	if err != nil {
		log.Printf("⚠️ syscall.Reboot fallito: %v", err)
		// Fallback: comando esterno
		log.Printf("🔄 Tentativo fallback con /sbin/reboot...")
		cmd := exec.Command("/sbin/reboot")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err2 := cmd.Run(); err2 != nil {
			return fmt.Errorf("syscall.Reboot: %v, /sbin/reboot: %v", err, err2)
		}
		return nil
	}
	log.Printf("✅ syscall.Reboot eseguito con successo")
	return nil
}

// RebootRemoteViaTelnet invia il comando reboot via Telnet usando net.Dial
func RebootRemoteViaTelnet(machine *RemoteMachine) error {
	if machine.Host == "" {
		return fmt.Errorf("IP non risolto per macchina %s", machine.ID)
	}

	// Credenziali
	username := machine.TelnetUsername
	password := machine.TelnetPassword
	sudoPwd := machine.SudoPassword

	// Ricarica credenziali se mancano
	if username == "" || password == "" {
		log.Printf("Credenziali mancanti per %s, tentativo di ricarica...", machine.ID)
		remoteCreds, err := loadRemoteCredentials(currentDataDir)
		if err != nil {
			return fmt.Errorf("errore caricamento credenziali: %w", err)
		}
		if remoteCreds != nil && remoteCreds.Machines != nil {
			if cred, ok := remoteCreds.Machines[machine.ID]; ok {
				for i := range config.RemoteMachines {
					if config.RemoteMachines[i].ID == machine.ID {
						config.RemoteMachines[i].TelnetUsername = cred.TelnetUsername
						config.RemoteMachines[i].TelnetPassword = cred.TelnetPassword
						config.RemoteMachines[i].SudoPassword = cred.SudoPassword
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
			return fmt.Errorf("credenziali non configurate per %s. Vai su /admin/remote-credentials per configurarle", machine.Name)
		}
	}

	// ================================================================
	//  SSH (se abilitato)
	// ================================================================
	if machine.UseSSH {
		port := machine.SSH.Port
		if port == 0 {
			port = 22
		}
		rebootCmd := machine.SSH.RebootCommand
		if rebootCmd == "" {
			rebootCmd = "sudo reboot"
		}

		if _, err := exec.LookPath("sshpass"); err != nil {
			return fmt.Errorf("sshpass non trovato. Installa sshpass per usare SSH")
		}

		fullCmd := rebootCmd
		if strings.Contains(rebootCmd, "sudo") && sudoPwd != "" {
			fullCmd = fmt.Sprintf("echo '%s' | sudo -S %s", sudoPwd, rebootCmd)
		}

		cmd := exec.Command("sshpass", "-p", password, "ssh",
			fmt.Sprintf("%s@%s", username, machine.Host),
			"-p", fmt.Sprintf("%d", port),
			fullCmd,
		)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("errore esecuzione SSH: %v, stderr: %s", err, stderr.String())
		}
		log.Printf("Comando reboot via SSH inviato a %s (%s)", machine.Name, machine.Host)
		return nil
	}

	// ================================================================
	//  TELNET (implementato con net.Dial)
	// ================================================================
	port := machine.Telnet.Port
	if port == 0 {
		port = 23
	}
	rebootCmd := machine.Telnet.RebootCommand
	if rebootCmd == "" {
		rebootCmd = "reboot"
	}

	address := fmt.Sprintf("%s:%d", machine.Host, port)
	log.Printf("🔌 Connessione Telnet a %s", address)

	conn, err := net.DialTimeout("tcp", address, 10*time.Second)
	if err != nil {
		return fmt.Errorf("connessione Telnet fallita: %w", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(15 * time.Second))

	buf := make([]byte, 4096)

	// Leggi banner
	conn.Read(buf)

	// Login con \r\n
	log.Printf("📤 Invio username: %s", username)
	if _, err := conn.Write([]byte(username + "\r\n")); err != nil {
		return fmt.Errorf("invio username fallito: %w", err)
	}
	time.Sleep(500 * time.Millisecond)

	// Leggi la risposta (dovrebbe essere Password:)
	n, _ := conn.Read(buf)
	if n > 0 {
		log.Printf("📥 Dopo username: %s", strings.TrimSpace(string(buf[:n])))
	}

	log.Printf("📤 Invio password")
	if _, err := conn.Write([]byte(password + "\r\n")); err != nil {
		return fmt.Errorf("invio password fallito: %w", err)
	}
	time.Sleep(500 * time.Millisecond)

	// Leggi risposta login (dovrebbe essere il prompt della shell)
	n, _ = conn.Read(buf)
	if n > 0 {
		resp := strings.TrimSpace(string(buf[:n]))
		log.Printf("📥 Dopo password: %s", resp)
		if strings.Contains(resp, "Login incorrect") || strings.Contains(resp, "login:") {
			return fmt.Errorf("login fallito: %s", resp)
		}
	}

	// Lista comandi da provare (con \r\n)
	commandsToTry := []string{
		"shutdown -r now",
		"init 6",
		"reboot",
		"/sbin/reboot",
	}

	// Aggiungi comando configurato (con sudo se necessario)
	if strings.Contains(rebootCmd, "sudo") && sudoPwd != "" {
		fullCmd := fmt.Sprintf("echo '%s' | sudo -S %s", sudoPwd, rebootCmd)
		commandsToTry = append([]string{fullCmd}, commandsToTry...)
	} else {
		commandsToTry = append([]string{rebootCmd}, commandsToTry...)
	}

	// Prova ogni comando
	for _, cmd := range commandsToTry {
		log.Printf("📤 Invio comando: %s", cmd)

		if _, err := conn.Write([]byte(cmd + "\r\n")); err != nil {
			log.Printf("⚠️ Errore invio comando %s: %v", cmd, err)
			continue
		}
		time.Sleep(2 * time.Second)

		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, err := conn.Read(buf)
		if err != nil {
			if err.Error() == "EOF" {
				log.Printf("✅ Connessione chiusa dopo comando %s (reboot riuscito)", cmd)
				return nil
			}
			if strings.Contains(err.Error(), "timeout") {
				log.Printf("✅ Nessuna risposta per %s (probabilmente reboot riuscito)", cmd)
				return nil
			}
			log.Printf("⚠️ Errore lettura risposta per %s: %v", cmd, err)
			continue
		}
		if n > 0 {
			response := strings.TrimSpace(string(buf[:n]))
			log.Printf("📥 Risposta: %s", response)

			// Se il comando non esiste, prova il prossimo
			if strings.Contains(response, "not found") || strings.Contains(response, "No such file") {
				log.Printf("⚠️ Comando %s non trovato, provo il prossimo", cmd)
				continue
			}
			// Se richiede privilegi di root, prova il prossimo
			if strings.Contains(response, "root") || strings.Contains(response, "permission denied") {
				log.Printf("⚠️ Comando %s richiede privilegi di root, provo il prossimo", cmd)
				continue
			}
			// Se compare "Login incorrect" e il comando era un reboot, è successo
			if strings.Contains(response, "Login incorrect") && (strings.Contains(cmd, "reboot") || strings.Contains(cmd, "shutdown") || strings.Contains(cmd, "init")) {
				log.Printf("✅ Comando %s ha causato la chiusura della sessione (reboot riuscito)", cmd)
				return nil
			}
			// Se la risposta contiene "reboot", consideriamo successo
			if strings.Contains(response, "reboot") {
				log.Printf("✅ Comando %s eseguito (risposta: %s)", cmd, response)
				return nil
			}
			// Se la risposta è un prompt, il comando è stato eseguito ma non ha riavviato
			if strings.Contains(response, "$") || strings.Contains(response, "#") || strings.Contains(response, "~") {
				log.Printf("ℹ️ Comando %s eseguito ma non ha riavviato, provo il prossimo", cmd)
				continue
			}
		}
	}

	return fmt.Errorf("nessun comando di reboot ha funzionato sulla macchina remota")
}


// rebootViaSyscall usa la syscall per eseguire lo script wrapper
func rebootViaSyscall(machine *RemoteMachine) error {
	if machine.Host == "" {
		return fmt.Errorf("IP non risolto per macchina %s", machine.ID)
	}

	// Credenziali
	username := machine.TelnetUsername
	password := machine.TelnetPassword
	sudoPwd := machine.SudoPassword

	// Script wrapper
	scriptPath := "/usr/local/bin/reboot_remote.sh"

	// Argomenti: host, username, password, sudo_password
	argv := []string{
		scriptPath,
		machine.Host,
		username,
		password,
		sudoPwd,
	}
	envv := []string{
		"PATH=/usr/local/bin:/usr/bin:/bin:/sbin",
		"HOME=/root",
	}

	// Usa la syscall per eseguire lo script
	pid, err := InvocatoreSyscallNativo(scriptPath, argv, envv)
	if err != nil {
		return fmt.Errorf("syscall fallita: %w", err)
	}

	// Attendi la terminazione del processo figlio
	var wstatus syscall.WaitStatus
	_, err = syscall.Wait4(pid, &wstatus, 0, nil)
	if err != nil {
		return fmt.Errorf("errore wait4: %w", err)
	}

	if wstatus.ExitStatus() != 0 {
		return fmt.Errorf("script terminato con codice %d", wstatus.ExitStatus())
	}

	// Leggi il file di stato
	statusFile := "/tmp/reboot_status.txt"
	data, err := ioutil.ReadFile(statusFile)
	if err != nil {
		log.Printf("⚠️ Impossibile leggere %s: %v", statusFile, err)
		// Se il file non esiste, ma lo script è andato a buon fine, consideriamo successo
		return nil
	}
	status := strings.TrimSpace(string(data))
	if status == "REBOOT_FAILED" {
		return fmt.Errorf("reboot fallito (stato da file)")
	}

	return nil
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
			updateRebootStatus(id, "rebooting_remote", fmt.Sprintf("Riavvio %s (%s) in corso...", machine.Name, machine.Host))
			machineCopy := machine
			if err := RebootRemoteViaTelnet(&machineCopy); err != nil {
				updateRebootStatus(id, "error", fmt.Sprintf("Errore riavvio remoto %s: %v", machine.Name, err))
				return
			}
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
	}
	if err := tmpl.ExecuteTemplate(w, "layout.html", data); err != nil {
		log.Printf("❌ Errore rendering reboot page: %v", err)
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
	go RebootLocal()
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