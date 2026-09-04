package main

import (
	"fmt"
	"log"
	"net/http" 
	"os"
	"path/filepath"
	"sync"
	"time"
)

var auditMutex sync.Mutex

// WriteAuditLog scrive una riga di log nel file giornaliero (senza contesto HTTP)
func WriteAuditLog(action, username, details string) {
	if config.AuditLogDir == "" {
		return
	}
	// Crea directory se non esiste
	if err := os.MkdirAll(config.AuditLogDir, 0755); err != nil {
		log.Printf("[ERROR] Errore creazione directory audit log: %v", err)
		return
	}
	// Nome file: LogMP48Ws_YYYYMMDD.log
	filename := fmt.Sprintf("LogMP48Ws_%s.log", time.Now().Format("20060102"))
	filePath := filepath.Join(config.AuditLogDir, filename)
	// Riga da scrivere: timestamp | operatore | azione | dettagli
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	line := fmt.Sprintf("%s | %s | %s | %s\n", timestamp, username, action, details)
	auditMutex.Lock()
	defer auditMutex.Unlock()
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("[ERROR] Errore apertura file audit log: %v", err)
		return
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		log.Printf("[ERROR] Errore scrittura audit log: %v", err)
	}

	// 🔄 Sincronizzazione immediata dell'audit log sulle macchine remote (in background)
	go func() {
		time.Sleep(500 * time.Millisecond)
		if err := SyncFileToAllRemotes(filePath); err != nil {
			log.Printf("[ERROR] Errore sincronizzazione audit log (scrittura): %v", err)
		} else {
			log.Printf("[INFO] Audit log sincronizzato dopo scrittura di '%s' da %s", action, username)
		}
	}()
}

// WriteAuditLogWithRequest scrive una riga di audit log arricchita con IP e User Agent
func WriteAuditLogWithRequest(action, username, details string, r *http.Request) {
	if r == nil {
		WriteAuditLog(action, username, details)
		return
	}
	ip := GetClientIP(r)
	ua := GetUserAgent(r)
	enrichedDetails := fmt.Sprintf("%s | IP: %s | UA: %s", details, ip, ua)
	WriteAuditLog(action, username, enrichedDetails)
}