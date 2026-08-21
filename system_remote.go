package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"strings"
	"syscall"
	"time"
	"net"
)

// RemoteSystemInfo contiene le informazioni di sistema di una macchina remota
type RemoteSystemInfo struct {
	MachineName string `json:"machineName"`
	Host        string `json:"host"`
	CPU         string `json:"cpu"`
	MemTotal    string `json:"memTotal"`
	MemUsed     string `json:"memUsed"`
	MemFree     string `json:"memFree"`
	DiskTotal   string `json:"diskTotal"`
	DiskUsed    string `json:"diskUsed"`
	DiskFree    string `json:"diskFree"`
	Uptime      string `json:"uptime"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
}

// getRemoteInfoViaSyscall esegue lo script wrapper per le info remote
func getRemoteInfoViaSyscall(machine RemoteMachine) (string, error) {
	if machine.Host == "" {
		return "", fmt.Errorf("IP non risolto per macchina %s", machine.ID)
	}

	username := machine.TelnetUsername
	password := machine.TelnetPassword

	// Ricarica credenziali se mancano
	if username == "" || password == "" {
		log.Printf("Credenziali mancanti per %s, tentativo di ricarica...", machine.ID)
		remoteCreds, err := loadRemoteCredentials(currentDataDir)
		if err != nil {
			return "", err
		}
		if remoteCreds != nil && remoteCreds.Machines != nil {
			if cred, ok := remoteCreds.Machines[machine.ID]; ok {
				machine.TelnetUsername = cred.TelnetUsername
				machine.TelnetPassword = cred.TelnetPassword
				username = cred.TelnetUsername
				password = cred.TelnetPassword
				log.Printf("✅ Credenziali ricaricate per %s", machine.ID)
			}
		}
	}

	if username == "" || password == "" {
		return "", fmt.Errorf("credenziali non configurate per %s. Vai su /admin/remote-credentials per configurarle", machine.Name)
	}

	// Prendi lo script path dalla configurazione
	scriptPath := machine.Telnet.ScriptInfoPath
	if scriptPath == "" {
		// Fallback per retrocompatibilità
		scriptPath = "/usr/local/bin/remote_info.sh"
		log.Printf("⚠️ ScriptInfoPath non configurato per %s, uso fallback: %s", machine.Name, scriptPath)
	}

	argv := []string{
		scriptPath,
		machine.Host,
		username,
		password,
	}
	envv := []string{
		"PATH=/usr/local/bin:/usr/bin:/bin:/sbin",
		"HOME=/root",
	}

	// Usa la syscall (funzione definita in esecutore_sys.go)
	pid, err := InvocatoreSyscallNativo(scriptPath, argv, envv)
	if err != nil {
		return "", fmt.Errorf("syscall fallita: %w", err)
	}

	var wstatus syscall.WaitStatus
	_, err = syscall.Wait4(pid, &wstatus, 0, nil)
	if err != nil {
		return "", fmt.Errorf("errore wait4: %w", err)
	}

	if wstatus.ExitStatus() != 0 {
		return "", fmt.Errorf("script terminato con codice %d", wstatus.ExitStatus())
	}

	// Leggi il file di output
	outputFile := "/tmp/remote_info.txt"
	data, err := ioutil.ReadFile(outputFile)
	if err != nil {
		return "", fmt.Errorf("errore lettura output: %w", err)
	}

	output := string(data)
	if strings.Contains(output, "INFO_FAILED") {
		return "", fmt.Errorf("script fallito")
	}

	return output, nil
}

// GetRemoteSystemInfo raccoglie le informazioni di sistema da una macchina remota
func GetRemoteSystemInfo(machine RemoteMachine) RemoteSystemInfo {
	info := RemoteSystemInfo{
		MachineName: machine.Name,
		Host:        machine.Host,
		Status:      "offline",
	}

	if machine.Host == "" {
		info.Error = "IP non risolto"
		return info
	}

	// Usa la syscall per ottenere le info
	output, err := getRemoteInfoViaSyscall(machine)
	if err != nil {
		info.Error = fmt.Sprintf("Errore recupero info: %v", err)
		return info
	}

	// Parser dell'output dello script
	info.Uptime = parseUptime2(output)
	info.CPU = parseLoadAvg2(output)
	info.MemTotal, info.MemUsed, info.MemFree = parseMemory2(output)
	info.DiskTotal, info.DiskUsed, info.DiskFree = parseDisk2(output)

	info.Status = "online"
	return info
}

// ==================== FUNZIONI DI PARSING (invariate) ====================

// execTelnetCommand2 esegue un comando e restituisce l'output pulito
// (mantenuta per compatibilità, ma non più usata)
func execTelnetCommand2(conn net.Conn, cmd string) (string, error) {
	conn.Write([]byte(cmd + "\n"))
	time.Sleep(800 * time.Millisecond)
	buf := make([]byte, 8192)
	n, err := conn.Read(buf)
	if err != nil {
		return "", err
	}
	output := string(buf[:n])

	lines := strings.Split(output, "\n")
	var cleaned []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(line, cmd) {
			continue
		}
		if strings.Contains(line, "$") || strings.Contains(line, "#") {
			continue
		}
		if strings.Contains(line, "login:") || strings.Contains(line, "Password:") {
			continue
		}
		cleaned = append(cleaned, line)
	}
	return strings.Join(cleaned, "\n"), nil
}

// parseUptime2 estrae l'uptime da "uptime"
func parseUptime2(output string) string {
	if strings.Contains(output, "up ") {
		parts := strings.Split(output, "up ")
		if len(parts) >= 2 {
			parts2 := strings.Split(parts[1], ",")
			if len(parts2) >= 1 {
				return strings.TrimSpace(parts2[0])
			}
		}
	}
	return "N/D"
}

// parseLoadAvg2 estrae il carico medio
func parseLoadAvg2(output string) string {
	if strings.Contains(output, "load average:") {
		parts := strings.Split(output, "load average:")
		if len(parts) >= 2 {
			return strings.TrimSpace(parts[1])
		}
	}
	return "N/D"
}

// parseMemory2 estrae memoria totale, usata e libera da "free -m"
func parseMemory2(output string) (string, string, string) {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Mem:") {
			fields := strings.Fields(line)
			if len(fields) >= 7 {
				total := fields[1]
				used := fields[2]
				free := fields[3]
				return total + " MB", used + " MB", free + " MB"
			}
		}
	}
	return "N/D", "N/D", "N/D"
}

// parseDisk2 estrae spazio totale, usato e libero da "df -h /"
func parseDisk2(output string) (string, string, string) {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "/") && !strings.Contains(line, "Filesystem") {
			fields := strings.Fields(line)
			if len(fields) >= 6 {
				total := fields[1]
				used := fields[2]
				free := fields[3]
				return total, used, free
			}
		}
	}
	return "N/D", "N/D", "N/D"
}