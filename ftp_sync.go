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
)

// FTPUploadFile carica un singolo file via FTP usando solo la standard library.
func FTPUploadFile(machine RemoteMachine, localPath, remotePath string) error {
	if machine.FTP.Host == "" {
		return fmt.Errorf("configurazione FTP mancante per %s", machine.Name)
	}
	addr := fmt.Sprintf("%s:%d", machine.FTP.Host, machine.FTP.Port)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Legge il messaggio di benvenuto
	if err := readFTPResponse(conn); err != nil {
		return err
	}

	// Invia USER
	if err := sendFTPCommand(conn, "USER "+machine.FTP.Username); err != nil {
		return err
	}
	if err := readFTPResponse(conn); err != nil {
		return err
	}

	// Invia PASS
	if err := sendFTPCommand(conn, "PASS "+machine.FTP.Password); err != nil {
		return err
	}
	if err := readFTPResponse(conn); err != nil {
		return err
	}

	// Attiva la modalità passiva
	port, err := passiveMode(conn)
	if err != nil {
		return err
	}

	// Crea le directory remote (simile a mkdir -p)
	if err := mkdirRecursiveFTP(conn, filepath.Dir(remotePath)); err != nil {
		log.Printf("Avviso creazione directory: %v", err)
	}

	// Apre il file locale
	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Invia il comando STOR
	if err := sendFTPCommand(conn, "STOR "+remotePath); err != nil {
		return err
	}
	// Legge la risposta preliminare (attesa)
	if err := readFTPResponse(conn); err != nil {
		return err
	}

	// Connessione dati
	dataConn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", machine.FTP.Host, port))
	if err != nil {
		return err
	}
	defer dataConn.Close()

	// Copia il file nel canale dati
	_, err = io.Copy(dataConn, file)
	if err != nil {
		return err
	}
	dataConn.Close()

	// Legge la risposta finale (completamento trasferimento)
	if err := readFTPResponse(conn); err != nil {
		return err
	}

	return nil
}

// mkdirRecursiveFTP crea ricorsivamente le directory remote.
func mkdirRecursiveFTP(conn net.Conn, remoteDir string) error {
	if remoteDir == "/" || remoteDir == "." {
		return nil
	}
	// Prova a cambiare directory; se riesce, esiste già
	if err := sendFTPCommand(conn, "CWD "+remoteDir); err != nil {
		// Ignora e prova a creare i genitori
	}
	if err := readFTPResponse(conn); err == nil {
		// La directory esiste
		return nil
	}
	// Crea i genitori
	parent := filepath.Dir(remoteDir)
	if parent != remoteDir {
		if err := mkdirRecursiveFTP(conn, parent); err != nil {
			return err
		}
	}
	// Crea la directory corrente
	if err := sendFTPCommand(conn, "MKD "+remoteDir); err != nil {
		return err
	}
	if err := readFTPResponse(conn); err != nil {
		// Ignora errore se già esiste
	}
	return nil
}

// passiveMode esegue il comando PASV e restituisce la porta per la connessione dati.
func passiveMode(conn net.Conn) (int, error) {
	if err := sendFTPCommand(conn, "PASV"); err != nil {
		return 0, err
	}
	response, err := readFTPResponseString(conn)
	if err != nil {
		return 0, err
	}
	// Estrae l'indirizzo e la porta dal formato: 227 Entering Passive Mode (h1,h2,h3,h4,p1,p2)
	start := strings.Index(response, "(")
	end := strings.LastIndex(response, ")")
	if start == -1 || end == -1 {
		return 0, fmt.Errorf("risposta PASV non valida: %s", response)
	}
	parts := strings.Split(response[start+1:end], ",")
	if len(parts) != 6 {
		return 0, fmt.Errorf("formato PASV errato: %s", response)
	}
	p1, _ := strconv.Atoi(parts[4])
	p2, _ := strconv.Atoi(parts[5])
	port := p1*256 + p2
	return port, nil
}

// sendFTPCommand invia un comando FTP al server.
func sendFTPCommand(conn net.Conn, cmd string) error {
	_, err := conn.Write([]byte(cmd + "\r\n"))
	return err
}

// readFTPResponse legge la risposta e verifica che il codice sia 2xx (successo).
func readFTPResponse(conn net.Conn) error {
	response, err := readFTPResponseString(conn)
	if err != nil {
		return err
	}
	// Controlla il codice di risposta (es. "226")
	if len(response) < 3 {
		return fmt.Errorf("risposta FTP troppo corta: %s", response)
	}
	code := response[:3]
	if code[0] != '2' && code[0] != '1' {
		return fmt.Errorf("errore FTP: %s", response)
	}
	return nil
}

// readFTPResponseString legge la risposta (può essere multi-linea) e restituisce l'ultima riga.
func readFTPResponseString(conn net.Conn) (string, error) {
	var fullResponse strings.Builder
	buf := make([]byte, 256)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return "", err
		}
		data := string(buf[:n])
		fullResponse.WriteString(data)
		// Controlla se la risposta è completa (codice + spazio)
		if strings.Contains(data, "\r\n") {
			// Analisi semplice: estrae l'ultima riga
			lines := strings.Split(fullResponse.String(), "\r\n")
			lastLine := lines[len(lines)-2]
			if len(lastLine) >= 3 && (lastLine[3] == ' ') {
				// La risposta è completa
				break
			}
		}
	}
	return strings.TrimSpace(fullResponse.String()), nil
}

// FTPUploadDirectory carica ricorsivamente una directory locale nel percorso remoto.
func FTPUploadDirectory(machine RemoteMachine, localDir, remoteDir string) error {
	return filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(localDir, path)
		if err != nil {
			return err
		}
		remotePath := filepath.Join(remoteDir, relPath)
		if info.IsDir() {
			// Le directory vengono create automaticamente durante l'upload dei file
			return nil
		}
		return FTPUploadFile(machine, path, remotePath)
	})
}

// Esempio di chiamata (da scommentare e usare nei punti di modifica)
/*
func syncToRemote(machine RemoteMachine, localPath, remotePath string) {
    go func() {
        if err := FTPUploadFile(machine, localPath, remotePath); err != nil {
            log.Printf("Errore sincronizzazione FTP verso %s: %v", machine.Name, err)
        } else {
            log.Printf("Sincronizzato %s verso %s", localPath, machine.Name)
        }
    }()
}
*/
