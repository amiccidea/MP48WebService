package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	syncMutex      sync.Mutex
	syncInProgress bool
)

// ==================== PAGINA SINCORONIZZAZIONE ====================

// syncPageHandler visualizza la pagina di sincronizzazione
func syncPageHandler(w http.ResponseWriter, r *http.Request) {
	username, isAdmin := getUserContext(r)
	if username == "" {
		http.Redirect(w, r, "/logout", http.StatusFound)
		return
	}
	if !isAdmin {
		http.Error(w, "Accesso negato", http.StatusForbidden)
		return
	}

	syncLogPath := filepath.Join(config.AuditLogDir, "sync.log")
	if config.AuditLogDir == "" {
		syncLogPath = "sync.log"
	}

	perms := getUserPermissions(username)
	data := struct {
		Username        string
		IsAdmin         bool
		Title           string
		ContentTemplate string
		Permissions     map[string]bool
		IsMultiCPU      bool
		SyncLogPath     string
	}{
		Username:        username,
		IsAdmin:         isAdmin,
		Title:           "Sincronizzazione CPU",
		ContentTemplate: "syncContent",
		Permissions:     perms,
		IsMultiCPU:      isMultiCPU(),
		SyncLogPath:     syncLogPath,
	}
	tmpl.ExecuteTemplate(w, "layout.html", data)
}

// ==================== ENDPOINT AVVIO SINCORONIZZAZIONE ====================

// syncAllRemotesHandler avvia la sincronizzazione manuale
func syncAllRemotesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_, isAdmin := getUserContext(r)
	if !isAdmin {
		http.Error(w, "Accesso negato", http.StatusForbidden)
		return
	}

	syncMutex.Lock()
	if syncInProgress {
		syncMutex.Unlock()
		http.Error(w, "Sincronizzazione già in corso", http.StatusConflict)
		return
	}
	syncInProgress = true
	syncMutex.Unlock()

	go func() {
		syncLogger := NewSyncLogger()
		defer syncLogger.Close()
		defer func() {
			syncMutex.Lock()
			syncInProgress = false
			syncMutex.Unlock()
		}()

		syncLogger.Info("🔄 Avvio sincronizzazione manuale completa...")

		// 1. Configurazione corrente
		if config.CurrentConfigurationDir != "" {
			syncLogger.Info("📤 Sincronizzazione configurazione: " + config.CurrentConfigurationDir)
			if err := SyncDirToAllRemotesWithLogger(config.CurrentConfigurationDir, syncLogger); err != nil {
				syncLogger.Error("❌ Errore sincronizzazione configurazione: " + err.Error())
			} else {
				syncLogger.Success("✅ Configurazione sincronizzata")
			}
		}

		// 2. Backup
		if config.ConfigHistoryDir != "" {
			syncLogger.Info("📤 Sincronizzazione backup: " + config.ConfigHistoryDir)
			if err := SyncDirToAllRemotesWithLogger(config.ConfigHistoryDir, syncLogger); err != nil {
				syncLogger.Error("❌ Errore sincronizzazione backup: " + err.Error())
			} else {
				syncLogger.Success("✅ Backup sincronizzati")
			}
		}

		// 3. Dati (utenti, ruoli, credenziali)
		if currentDataDir != "" {
			syncLogger.Info("📤 Sincronizzazione dati (utenti/ruoli/credenziali): " + currentDataDir)
			if err := SyncDirToAllRemotesWithLogger(currentDataDir, syncLogger); err != nil {
				syncLogger.Error("❌ Errore sincronizzazione dati: " + err.Error())
			} else {
				syncLogger.Success("✅ Dati sincronizzati")
			}
		}

		// 4. Audit log
		if config.AuditLogDir != "" {
			syncLogger.Info("📤 Sincronizzazione audit log: " + config.AuditLogDir)
			if err := SyncDirToAllRemotesWithLogger(config.AuditLogDir, syncLogger); err != nil {
				syncLogger.Error("❌ Errore sincronizzazione audit log: " + err.Error())
			} else {
				syncLogger.Success("✅ Audit log sincronizzati")
			}
		}

		// 5. Log applicativi
		for _, cat := range config.LogCategories {
			for _, dir := range cat.Directories {
				syncLogger.Info(fmt.Sprintf("📤 Sincronizzazione log (%s): %s", cat.Name, dir))
				if err := SyncDirToAllRemotesWithLogger(dir, syncLogger); err != nil {
					syncLogger.Error(fmt.Sprintf("❌ Errore sincronizzazione log %s: %v", dir, err))
				} else {
					syncLogger.Success(fmt.Sprintf("✅ Log sincronizzati: %s", dir))
				}
			}
		}

		// Messaggio finale di completamento
		syncLogger.Success("✅ Sincronizzazione manuale completa terminata")

		// Invia un messaggio speciale per indicare che la sincronizzazione è finita
		// e il client può chiudere la connessione
		time.Sleep(200 * time.Millisecond)
		SSEBroadcast(SyncMessage{
			Type:    "done",
			Message: "Connessione chiusa dal server",
			Time:    time.Now().Format("15:04:05"),
		})
	}()

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Sincronizzazione avviata in background"))
}

// ==================== SERVER-SENT EVENTS ====================

type SyncMessage struct {
	Type    string `json:"type"` // "info", "success", "warning", "error", "done"
	Message string `json:"message"`
	Time    string `json:"time"`
}

var (
	clients   = make(map[chan SyncMessage]bool)
	clientsMu sync.RWMutex
)

// SSEBroadcast invia un messaggio a tutti i client connessi
func SSEBroadcast(msg SyncMessage) {
	clientsMu.RLock()
	defer clientsMu.RUnlock()
	for client := range clients {
		select {
		case client <- msg:
		default:
			// Se il canale è pieno, lo salta
		}
	}
}

// syncEventsHandler gestisce le connessioni SSE
func syncEventsHandler(w http.ResponseWriter, r *http.Request) {
	_, isAdmin := getUserContext(r)
	if !isAdmin {
		http.Error(w, "Accesso negato", http.StatusForbidden)
		return
	}

	// Header per Server-Sent Events
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming non supportato", http.StatusInternalServerError)
		return
	}

	// Crea un canale per questo client
	clientChan := make(chan SyncMessage, 10)
	clientsMu.Lock()
	clients[clientChan] = true
	clientsMu.Unlock()

	// Rimuovi il client quando la funzione termina
	defer func() {
		clientsMu.Lock()
		delete(clients, clientChan)
		clientsMu.Unlock()
		close(clientChan)
	}()

	// Invia gli eventi al client
	for msg := range clientChan {
		data, err := json.Marshal(msg)
		if err != nil {
			continue
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()

		// Se riceviamo un messaggio "done", chiudiamo la connessione
		if msg.Type == "done" {
			return
		}
	}
}

// ==================== SISTEMA DI LOGGING ====================

// SyncLogger gestisce la scrittura su file e l'invio SSE
type SyncLogger struct {
	file   *os.File
	events chan SyncMessage
}

// NewSyncLogger crea un nuovo logger e avvia il broadcaster
func NewSyncLogger() *SyncLogger {
	logPath := filepath.Join(config.AuditLogDir, "sync.log")
	if config.AuditLogDir == "" {
		logPath = "sync.log"
	}
	os.MkdirAll(filepath.Dir(logPath), 0755)

	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Printf("Errore apertura sync.log: %v", err)
		file = nil
	}

	sl := &SyncLogger{
		file:   file,
		events: make(chan SyncMessage, 100),
	}

	// Avvia il broadcaster in background
	go sl.broadcastEvents()
	return sl
}

// broadcastEvents legge dal canale e scrive su file + SSE
func (sl *SyncLogger) broadcastEvents() {
	for msg := range sl.events {
		// Scrivi su file
		if sl.file != nil {
			line := fmt.Sprintf("[%s] %s: %s\n", msg.Time, msg.Type, msg.Message)
			sl.file.WriteString(line)
			sl.file.Sync()
		}
		// Invia a tutti i client SSE
		SSEBroadcast(msg)
	}
}

// Info scrive un messaggio informativo
func (sl *SyncLogger) Info(msg string) {
	sl.events <- SyncMessage{Type: "info", Message: msg, Time: time.Now().Format("15:04:05")}
}

// Success scrive un messaggio di successo
func (sl *SyncLogger) Success(msg string) {
	sl.events <- SyncMessage{Type: "success", Message: msg, Time: time.Now().Format("15:04:05")}
}

// Error scrive un messaggio di errore
func (sl *SyncLogger) Error(msg string) {
	sl.events <- SyncMessage{Type: "error", Message: msg, Time: time.Now().Format("15:04:05")}
}

// Warning scrive un messaggio di avviso
func (sl *SyncLogger) Warning(msg string) {
	sl.events <- SyncMessage{Type: "warning", Message: msg, Time: time.Now().Format("15:04:05")}
}

// Close chiude il logger
func (sl *SyncLogger) Close() {
	close(sl.events)
	if sl.file != nil {
		sl.file.Close()
	}
}

// ==================== FUNZIONI HELPER CON LOGGER ====================

// SyncDirToAllRemotesWithLogger sincronizza una directory con logging progressivo
func SyncDirToAllRemotesWithLogger(localDir string, logger *SyncLogger) error {
	var lastErr error
	for _, machine := range config.RemoteMachines {
		if machine.ID == "local" {
			continue
		}
		if machine.FTP.Username == "" || machine.FTP.Password == "" {
			logger.Warning("⚠️ Credenziali FTP mancanti per " + machine.Name + ", salto...")
			continue
		}
		remoteDir := buildRemotePath(localDir, machine.FTP.Username)
		logger.Info(fmt.Sprintf("📤 Caricamento %s su %s (%s)", localDir, machine.Name, machine.Host))

		var err error
		for attempt := 1; attempt <= 3; attempt++ {
			err = FTPUploadDirectory(machine, localDir, remoteDir)
			if err == nil {
				break
			}
			logger.Warning(fmt.Sprintf("⚠️ Tentativo %d/3 fallito per %s", attempt, machine.Name))
			time.Sleep(2 * time.Second)
		}
		if err != nil {
			logger.Error(fmt.Sprintf("❌ Errore upload su %s: %v", machine.Name, err))
			lastErr = err
			continue
		}
		logger.Success(fmt.Sprintf("✅ Directory caricata su %s", machine.Name))
	}
	return lastErr
}
