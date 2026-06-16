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

	// Attiva modalità passiva (richiede PASV)
	dataPort, err := passiveMode(conn)
	if err != nil {
		return err
	}

	// Crea directory remote (se necessario)
	dir := filepath.Dir(remotePath)
	if err := mkdirRecursiveFTPNative(conn, dir); err != nil {
		log.Printf("Avviso creazione directory: %v", err)
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

// mkdirRecursiveFTPNative crea ricorsivamente le directory remote (versione nativa)
func mkdirRecursiveFTPNative(conn net.Conn, remoteDir string) error {
	if remoteDir == "/" || remoteDir == "." {
		return nil
	}
	if err := sendFTPCommand(conn, "CWD "+remoteDir); err == nil {
		return nil
	}
	parent := filepath.Dir(remoteDir)
	if parent != remoteDir {
		if err := mkdirRecursiveFTPNative(conn, parent); err != nil {
			return err
		}
	}
	if err := sendFTPCommand(conn, "MKD "+remoteDir); err != nil {
		return err
	}
	readFTPResponse(conn) // ignora errore
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
	if code[0] != '2' && code[0] != '1' {
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
			lastLine := lines[len(lines)-2]
			if len(lastLine) >= 3 && lastLine[3] == ' ' {
				break
			}
		}
	}
	return strings.TrimSpace(full.String()), nil
}

// FTPUploadDirectory carica ricorsivamente una directory
func FTPUploadDirectory(machine RemoteMachine, localDir, remoteDir string) error {
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
