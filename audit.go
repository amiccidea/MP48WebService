package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var auditMutex sync.Mutex

// WriteAuditLog scrive una riga di log nel file giornaliero
func WriteAuditLog(action, username, details string) {
	if config.AuditLogDir == "" {
		return // logging disabilitato
	}
	// Crea directory se non esiste
	if err := os.MkdirAll(config.AuditLogDir, 0755); err != nil {
		log.Printf("Errore creazione directory audit log: %v", err)
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
		log.Printf("Errore apertura file audit log: %v", err)
		return
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		log.Printf("Errore scrittura audit log: %v", err)
	}
}
