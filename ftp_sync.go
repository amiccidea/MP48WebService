package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"
)

// FTPUploadFile carica un singolo file via FTP
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

	conn, err := ftp.Dial(addr, ftp.DialWithTimeout(30*time.Second))
	if err != nil {
		return err
	}
	defer conn.Quit()

	if err := conn.Login(machine.FTP.Username, machine.FTP.Password); err != nil {
		return err
	}

	// remotePath deve essere assoluto (inizia con /)
	if !strings.HasPrefix(remotePath, "/") {
		remotePath = "/" + remotePath
	}
	remotePath = filepath.Clean(remotePath)

	// Crea la directory remota se non esiste (solo la directory del file)
	dir := filepath.Dir(remotePath)
	if err := createRemoteDir(conn, dir); err != nil {
		log.Printf("Avviso creazione directory %s: %v", dir, err)
		// Continuiamo comunque
	}

	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Usa il percorso assoluto per STOR
	return conn.Stor(remotePath, file)
}

// createRemoteDir crea ricorsivamente le directory remote
func createRemoteDir(conn *ftp.ServerConn, remoteDir string) error {
	if remoteDir == "" || remoteDir == "/" || remoteDir == "." {
		return nil
	}

	// Assicura che sia assoluto
	if !strings.HasPrefix(remoteDir, "/") {
		remoteDir = "/" + remoteDir
	}
	remoteDir = filepath.Clean(remoteDir)

	// Ottieni la directory corrente
	currentDir, err := conn.CurrentDir()
	if err != nil {
		return err
	}

	// Se remoteDir inizia con currentDir, rimuoviamo il prefisso per avere un percorso relativo
	relPath := remoteDir
	if strings.HasPrefix(remoteDir, currentDir) {
		relPath = strings.TrimPrefix(remoteDir, currentDir)
		relPath = strings.TrimPrefix(relPath, "/")
	}

	// Se relPath è vuoto, significa che la directory è la stessa di currentDir
	if relPath == "" {
		return nil
	}

	// Se relPath non inizia con currentDir, proviamo a cambiare in / e usare il percorso assoluto
	if !strings.HasPrefix(remoteDir, currentDir) {
		// Proviamo a cambiare in root
		if err := conn.ChangeDir("/"); err != nil {
			// Se non possiamo andare in /, usiamo il percorso relativo (ma potrebbe essere fuori dalla home)
			// In tal caso, potremmo non avere permessi, ma proviamo lo stesso.
			log.Printf("Avviso: non posso andare in /, uso percorso relativo: %s", relPath)
		} else {
			// Siamo in /, ora usiamo il percorso assoluto senza il primo slash
			relPath = strings.TrimPrefix(remoteDir, "/")
		}
	}

	// Dividi il percorso relativo in componenti
	parts := strings.Split(relPath, "/")
	for _, part := range parts {
		if part == "" {
			continue
		}
		// Prova a cambiare nella directory
		if err := conn.ChangeDir(part); err != nil {
			// Se fallisce, prova a crearla
			if err := conn.MakeDir(part); err != nil {
				// Se l'errore è "directory already exists", prova a cambiare di nuovo
				if strings.Contains(strings.ToLower(err.Error()), "exists") || strings.Contains(strings.ToLower(err.Error()), "already") {
					conn.ChangeDir(part)
					continue
				}
				return fmt.Errorf("errore creazione directory %s: %v", part, err)
			}
			// Dopo la creazione, cambia nella directory appena creata
			if err := conn.ChangeDir(part); err != nil {
				log.Printf("Avviso: non posso entrare in %s dopo averla creata", part)
			}
		}
	}
	return nil
}

// uploadFileWithConn carica un file usando una connessione già aperta
func uploadFileWithConn(conn *ftp.ServerConn, localPath, remotePath string) error {
	if !strings.HasPrefix(remotePath, "/") {
		remotePath = "/" + remotePath
	}
	remotePath = filepath.Clean(remotePath)

	dir := filepath.Dir(remotePath)
	if err := createRemoteDir(conn, dir); err != nil {
		log.Printf("Avviso creazione directory %s: %v", dir, err)
	}

	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer file.Close()

	return conn.Stor(remotePath, file)
}

// FTPUploadDirectory carica ricorsivamente una directory usando una sola connessione
func FTPUploadDirectory(machine RemoteMachine, localDir, remoteDir string) error {
	port := machine.FTP.Port
	if port == 0 {
		port = 21
	}
	addr := fmt.Sprintf("%s:%d", machine.Host, port)

	conn, err := ftp.Dial(addr, ftp.DialWithTimeout(30*time.Second))
	if err != nil {
		return err
	}
	defer conn.Quit()

	if err := conn.Login(machine.FTP.Username, machine.FTP.Password); err != nil {
		return err
	}

	// remoteDir deve essere assoluto
	if !strings.HasPrefix(remoteDir, "/") {
		remoteDir = "/" + remoteDir
	}
	remoteDir = filepath.Clean(remoteDir)

	var lastErr error
	err = filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Printf("Errore walk su %s: %v", path, err)
			return nil
		}
		relPath, _ := filepath.Rel(localDir, path)
		remotePath := filepath.Join(remoteDir, relPath)

		if info.IsDir() {
			log.Printf("📁 Creazione directory %s", remotePath)
			if err := createRemoteDir(conn, remotePath); err != nil {
				log.Printf("❌ Errore creazione directory %s: %v", remotePath, err)
				lastErr = err
				return nil
			}
			return nil
		}

		log.Printf("📤 Caricamento %s -> %s", path, remotePath)
		if err := uploadFileWithConn(conn, path, remotePath); err != nil {
			log.Printf("❌ Errore caricando %s: %v", path, err)
			lastErr = err
			return nil
		}
		return nil
	})
	if err != nil {
		return err
	}
	return lastErr
}

// buildRemotePath costruisce il percorso remoto assoluto sostituendo il nome utente
func buildRemotePath(localPath, ftpUsername string) string {
	if strings.HasPrefix(localPath, "/home/") {
		parts := strings.SplitN(localPath, "/", 4) // ["", "home", "andrea", "Documenti/..."]
		if len(parts) >= 4 {
			// Restituisci /home/andrea2/Documenti/...
			return "/" + filepath.Join(parts[1], ftpUsername, parts[3])
		}
	}
	// Se non inizia con /home/, aggiungi / all'inizio
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
		var err error
		for attempt := 1; attempt <= 3; attempt++ {
			err = FTPUploadFile(machine, localPath, remotePath)
			if err == nil {
				break
			}
			log.Printf("⚠️ Tentativo %d/3 fallito per %s: %v, riprovo...", attempt, machine.Name, err)
			time.Sleep(2 * time.Second)
		}
		if err != nil {
			log.Printf("❌ Errore upload su %s dopo 3 tentativi: %v", machine.Name, err)
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
		var err error
		for attempt := 1; attempt <= 3; attempt++ {
			err = FTPUploadDirectory(machine, localDir, remoteDir)
			if err == nil {
				break
			}
			log.Printf("⚠️ Tentativo %d/3 fallito per %s: %v, riprovo...", attempt, machine.Name, err)
			time.Sleep(3 * time.Second)
		}
		if err != nil {
			log.Printf("❌ Errore upload directory su %s dopo 3 tentativi: %v", machine.Name, err)
			lastErr = err
			continue
		}
		log.Printf("✅ Directory caricata su %s", machine.Name)
	}
	return lastErr
}

// FTPDeleteFile elimina un file remoto via FTP
func FTPDeleteFile(machine RemoteMachine, remotePath string) error {
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

	conn, err := ftp.Dial(addr, ftp.DialWithTimeout(30*time.Second))
	if err != nil {
		return err
	}
	defer conn.Quit()

	if err := conn.Login(machine.FTP.Username, machine.FTP.Password); err != nil {
		return err
	}

	// remotePath deve essere assoluto
	if !strings.HasPrefix(remotePath, "/") {
		remotePath = "/" + remotePath
	}
	remotePath = filepath.Clean(remotePath)

	// Verifica se il file esiste prima di eliminarlo
	if _, err := conn.FileSize(remotePath); err != nil {
		// Il file non esiste, non è un errore
		return nil
	}

	// Elimina il file
	return conn.Delete(remotePath)
}

// SyncFileDeleteFromAllRemotes elimina un file da tutte le macchine remote
func SyncFileDeleteFromAllRemotes(localPath string) error {
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
		log.Printf("🗑️ Eliminazione %s su %s (%s) -> %s", localPath, machine.Name, machine.Host, remotePath)

		var err error
		for attempt := 1; attempt <= 3; attempt++ {
			err = FTPDeleteFile(machine, remotePath)
			if err == nil {
				break
			}
			// Se il file non esiste, non è un errore
			if strings.Contains(err.Error(), "file does not exist") {
				log.Printf("ℹ️ File già eliminato su %s", machine.Name)
				return nil
			}
			log.Printf("⚠️ Tentativo %d/3 fallito per %s: %v, riprovo...", attempt, machine.Name, err)
			time.Sleep(2 * time.Second)
		}
		if err != nil {
			// Non consideriamo errore se il file non esiste
			if strings.Contains(err.Error(), "file does not exist") {
				log.Printf("ℹ️ File non presente su %s", machine.Name)
				continue
			}
			log.Printf("❌ Errore eliminazione su %s dopo 3 tentativi: %v", machine.Name, err)
			lastErr = err
			continue
		}
		log.Printf("✅ File eliminato su %s", machine.Name)
	}
	return lastErr
}
