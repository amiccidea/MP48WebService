package main

import (
	"fmt"
	"net"
	"strings"
	"time"
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

// GetRemoteSystemInfo raccoglie le informazioni di sistema da una macchina remota via Telnet
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

	if machine.Telnet.Username == "" || machine.Telnet.Password == "" {
		info.Error = "Credenziali Telnet non configurate"
		return info
	}

	port := machine.Telnet.Port
	if port == 0 {
		port = 23
	}
	addr := fmt.Sprintf("%s:%d", machine.Host, port)

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		info.Error = fmt.Sprintf("Connessione fallita: %v", err)
		return info
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(15 * time.Second))

	// Login
	conn.Write([]byte("\n"))
	time.Sleep(300 * time.Millisecond)
	conn.Write([]byte(machine.Telnet.Username + "\n"))
	time.Sleep(300 * time.Millisecond)
	conn.Write([]byte(machine.Telnet.Password + "\n"))
	time.Sleep(500 * time.Millisecond)

	// Leggi il banner/login (per pulire il buffer)
	conn.Read(make([]byte, 4096))
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	// ---- RACCOLTA DATI ----

	// 1. Uptime e carico medio
	if output, err := execTelnetCommand2(conn, "uptime"); err == nil {
		info.Uptime = parseUptime2(output)
		info.CPU = parseLoadAvg2(output)
	}

	// 2. Memoria (free -m)
	if output, err := execTelnetCommand2(conn, "free -m"); err == nil {
		info.MemTotal, info.MemUsed, info.MemFree = parseMemory2(output)
	}

	// 3. Disco (df -h /)
	if output, err := execTelnetCommand2(conn, "df -h /"); err == nil {
		info.DiskTotal, info.DiskUsed, info.DiskFree = parseDisk2(output)
	}

	info.Status = "online"
	return info
}

// execTelnetCommand2 esegue un comando e restituisce l'output pulito
func execTelnetCommand2(conn net.Conn, cmd string) (string, error) {
	// Invia il comando
	conn.Write([]byte(cmd + "\n"))
	time.Sleep(800 * time.Millisecond)

	// Leggi la risposta (buffer più grande)
	buf := make([]byte, 8192)
	n, err := conn.Read(buf)
	if err != nil {
		return "", err
	}
	output := string(buf[:n])

	// Pulisci l'output: rimuovi il comando stesso e il prompt
	lines := strings.Split(output, "\n")
	var cleaned []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Salta la riga che contiene il comando stesso
		if strings.Contains(line, cmd) {
			continue
		}
		// Salta righe che sembrano prompt (es. "user@host:~$", "user@host:~#")
		if strings.Contains(line, "$") || strings.Contains(line, "#") {
			continue
		}
		// Salta righe di login/banner
		if strings.Contains(line, "login:") || strings.Contains(line, "Password:") {
			continue
		}
		cleaned = append(cleaned, line)
	}
	return strings.Join(cleaned, "\n"), nil
}

// parseUptime2 estrae l'uptime da "uptime"
func parseUptime2(output string) string {
	// Esempio: " 10:00:00 up 2 days, 3:45,  1 user,  load average: 0.23, 0.45, 0.67"
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
