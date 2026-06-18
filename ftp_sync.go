package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// FTPUploadFile carica un singolo file via FTP usando la standard library.
func FTPUploadFile(machine RemoteMachine, localPath, remotePath string) error {
	if machine.Host == "" {
		return fmt.Errorf("IP non risolto per macchina %s", machine.ID)
	}
	if machine.FTP.Username == "" || machine.FTP.Password == "" {
		return fmt.Errorf("credenziali FTP non configurate per %s", machine.Name)
	}
	port := machine.FTP.Port
	if port == 0 {
		port = 21
	}
	addr := fmt.Sprintf("%s:%d", machine.Host, port)
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Leggi benvenuto
	if err := readFTPResponse(conn); err != nil {
		return err
	}
	// USER
	if err := sendFTPCommand(conn, "USER "+machine.FTP.Username); err != nil {
		return err
	}
	if err := readFTPResponse(conn); err != nil {
		return err
	}
	// PASS
	if err := sendFTPCommand(conn, "PASS "+machine.FTP.Password); err != nil {
		return err
	}
	if err := readFTPResponse(conn); err != nil {
		return err
	}

	// Attiva modalità passiva
	dataPort, err := passiveMode(conn)
	if err != nil {
		return err
	}

	// Crea directory remote (se necessario)
	dir := filepath.Dir(remotePath)
	if err := mkdirRecursiveFTPNative(conn, dir); err != nil {
		log.Printf("Avviso creazione directory: %v", err)
		// Continua comunque, forse la directory esiste già
	}

	// Apre il file locale
	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer file.Close()

	// STOR
	if err := sendFTPCommand(conn, "STOR "+remotePath); err != nil {
		return err
	}
	if err := readFTPResponse(conn); err != nil {
		return err
	}

	// Connessione dati
	dataConn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", machine.Host, dataPort), 10*time.Second)
	if err != nil {
		return err
	}
	defer dataConn.Close()

	if _, err := io.Copy(dataConn, file); err != nil {
		return err
	}
	dataConn.Close()

	// Risposta finale
	if err := readFTPResponse(conn); err != nil {
		return err
	}
	return nil
}

// mkdirRecursiveFTPNative crea ricorsivamente le directory remote (versione robusta)
func mkdirRecursiveFTPNative(conn net.Conn, remoteDir string) error {
	if remoteDir == "" || remoteDir == "/" || remoteDir == "." {
		return nil
	}

	// Se inizia con /, parti dalla root
	var parts []string
	var currentPath string
	if strings.HasPrefix(remoteDir, "/") {
		parts = strings.Split(strings.TrimPrefix(remoteDir, "/"), "/")
		currentPath = "/"
	} else {
		parts = strings.Split(remoteDir, "/")
		currentPath = ""
	}

	for _, part := range parts {
		if part == "" {
			continue
		}
		if currentPath == "/" {
			currentPath = "/" + part
		} else if currentPath == "" {
			currentPath = part
		} else {
			currentPath = currentPath + "/" + part
		}

		// Prova CWD
		if err := sendFTPCommand(conn, "CWD "+currentPath); err != nil {
			return fmt.Errorf("errore CWD: %w", err)
		}
		resp, err := readFTPResponseString(conn)
		if err != nil {
			return err
		}
		if len(resp) >= 3 && (resp[0] == '2' || resp[0] == '1') {
			continue
		}
		// Se 5xx, prova MKD
		if len(resp) >= 3 && resp[0] == '5' {
			if err := sendFTPCommand(conn, "MKD "+currentPath); err != nil {
				return fmt.Errorf("errore MKD: %w", err)
			}
			mkdResp, err := readFTPResponseString(conn)
			if err != nil {
				return err
			}
			if len(mkdResp) >= 3 && mkdResp[0] == '5' {
				if strings.Contains(mkdResp, "521") || strings.Contains(mkdResp, "exists") {
					continue
				}
				return fmt.Errorf("errore creazione directory %s: %s", currentPath, mkdResp)
			}
			log.Printf("Directory creata: %s", currentPath)
		}
	}
	return nil
}

// passiveMode esegue PASV e restituisce la porta dati
func passiveMode(conn net.Conn) (int, error) {
	if err := sendFTPCommand(conn, "PASV"); err != nil {
		return 0, err
	}
	resp, err := readFTPResponseString(conn)
	if err != nil {
		return 0, err
	}
	start := strings.Index(resp, "(")
	end := strings.LastIndex(resp, ")")
	if start == -1 || end == -1 {
		return 0, fmt.Errorf("risposta PASV non valida: %s", resp)
	}
	parts := strings.Split(resp[start+1:end], ",")
	if len(parts) != 6 {
		return 0, fmt.Errorf("formato PASV errato: %s", resp)
	}
	p1, _ := strconv.Atoi(parts[4])
	p2, _ := strconv.Atoi(parts[5])
	return p1*256 + p2, nil
}

func sendFTPCommand(conn net.Conn, cmd string) error {
	_, err := conn.Write([]byte(cmd + "\r\n"))
	return err
}

func readFTPResponse(conn net.Conn) error {
	resp, err := readFTPResponseString(conn)
	if err != nil {
		return err
	}
	if len(resp) < 3 {
		return fmt.Errorf("risposta FTP troppo corta: %s", resp)
	}
	code := resp[:3]
	// Accetta 1xx, 2xx e 3xx come risposte non negative
	if code[0] != '1' && code[0] != '2' && code[0] != '3' {
		return fmt.Errorf("errore FTP: %s", resp)
	}
	return nil
}

func readFTPResponseString(conn net.Conn) (string, error) {
	var full strings.Builder
	buf := make([]byte, 256)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return "", err
		}
		data := string(buf[:n])
		full.WriteString(data)
		if strings.Contains(data, "\r\n") {
			lines := strings.Split(full.String(), "\r\n")
			if len(lines) >= 2 {
				lastLine := lines[len(lines)-2]
				if len(lastLine) >= 3 && lastLine[3] == ' ' {
					break
				}
			}
		}
	}
	return strings.TrimSpace(full.String()), nil
}

// FTPUploadDirectory carica ricorsivamente una directory
func FTPUploadDirectory(machine RemoteMachine, localDir, remoteDir string) error {
	// Assicura che remoteDir sia assoluto
	if !strings.HasPrefix(remoteDir, "/") {
		remoteDir = "/" + remoteDir
	}
	return filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, _ := filepath.Rel(localDir, path)
		remotePath := filepath.Join(remoteDir, relPath)
		if info.IsDir() {
			return nil
		}
		return FTPUploadFile(machine, path, remotePath)
	})
}

// buildRemotePath sostituisce il nome utente nel percorso locale con quello FTP
func buildRemotePath(localPath, ftpUsername string) string {
	if strings.HasPrefix(localPath, "/home/") {
		parts := strings.SplitN(localPath, "/", 4)
		if len(parts) >= 4 {
			// Restituisci /home/andrea2/Documenti/...
			return "/" + filepath.Join(parts[1], ftpUsername, parts[3])
		}
	}
	// Per altri percorsi, aggiungi / se non c'è
	if !strings.HasPrefix(localPath, "/") {
		return "/" + localPath
	}
	return localPath
}

// SyncFileToAllRemotes carica un singolo file su tutte le macchine remote
func SyncFileToAllRemotes(localPath string) error {
	var lastErr error
	for _, machine := range config.RemoteMachines {
		if machine.ID == "local" {
			continue
		}
		if machine.FTP.Username == "" || machine.FTP.Password == "" {
			log.Printf("⚠️ Credenziali FTP mancanti per %s, salto...", machine.Name)
			continue
		}
		remotePath := buildRemotePath(localPath, machine.FTP.Username)
		log.Printf("📤 Caricamento %s su %s (%s) -> %s", localPath, machine.Name, machine.Host, remotePath)
		if err := FTPUploadFile(machine, localPath, remotePath); err != nil {
			log.Printf("❌ Errore upload su %s: %v", machine.Name, err)
			lastErr = err
			continue
		}
		log.Printf("✅ File caricato su %s", machine.Name)
	}
	return lastErr
}

// SyncDirToAllRemotes carica ricorsivamente una directory su tutte le macchine remote
func SyncDirToAllRemotes(localDir string) error {
	var lastErr error
	for _, machine := range config.RemoteMachines {
		if machine.ID == "local" {
			continue
		}
		if machine.FTP.Username == "" || machine.FTP.Password == "" {
			log.Printf("⚠️ Credenziali FTP mancanti per %s, salto...", machine.Name)
			continue
		}
		remoteDir := buildRemotePath(localDir, machine.FTP.Username)
		log.Printf("📤 Caricamento directory %s su %s (%s) -> %s", localDir, machine.Name, machine.Host, remoteDir)
		if err := FTPUploadDirectory(machine, localDir, remoteDir); err != nil {
			log.Printf("❌ Errore upload directory su %s: %v", machine.Name, err)
			lastErr = err
			continue
		}
		log.Printf("✅ Directory caricata su %s", machine.Name)
	}
	return lastErr
}
