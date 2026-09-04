package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
)

// Livelli di log
const (
	LevelInfo  = "INFO"
	LevelWarn  = "WARN"
	LevelError = "ERROR"
	LevelDebug = "DEBUG"
)

// logWithLevel scrive un messaggio con livello e timestamp (già gestito da log.Printf)
func logWithLevel(level, msg string) {
	log.Printf("[%s] %s", level, msg)
}

// Info logga un messaggio di livello INFO
func Info(msg string) {
	logWithLevel(LevelInfo, msg)
}

// Infof logga un messaggio formattato di livello INFO
func Infof(format string, args ...interface{}) {
	logWithLevel(LevelInfo, fmt.Sprintf(format, args...))
}

// Warn logga un messaggio di livello WARN
func Warn(msg string) {
	logWithLevel(LevelWarn, msg)
}

// Warnf logga un messaggio formattato di livello WARN
func Warnf(format string, args ...interface{}) {
	logWithLevel(LevelWarn, fmt.Sprintf(format, args...))
}

// Error logga un messaggio di livello ERROR
func Error(msg string) {
	logWithLevel(LevelError, msg)
}

// Errorf logga un messaggio formattato di livello ERROR
func Errorf(format string, args ...interface{}) {
	logWithLevel(LevelError, fmt.Sprintf(format, args...))
}

// Debug logga un messaggio di livello DEBUG (solo se config.DebugMode è true)
func Debug(msg string) {
	if config.DebugMode {
		logWithLevel(LevelDebug, msg)
	}
}

// Debugf logga un messaggio formattato di livello DEBUG (solo se config.DebugMode è true)
func Debugf(format string, args ...interface{}) {
	if config.DebugMode {
		logWithLevel(LevelDebug, fmt.Sprintf(format, args...))
	}
}

// GetClientIP estrae l'IP del client da una richiesta HTTP
func GetClientIP(r *http.Request) string {
	if r == nil {
		return "N/A"
	}
	// Prova X-Forwarded-For (se dietro proxy)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Potrebbe essere una lista di IP, prendi il primo
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	// Altrimenti usa RemoteAddr
	return r.RemoteAddr
}

// GetUserAgent estrae lo User Agent da una richiesta HTTP
func GetUserAgent(r *http.Request) string {
	if r == nil {
		return "N/A"
	}
	return r.UserAgent()
}