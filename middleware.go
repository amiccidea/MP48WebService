package main

import (
	"log"
	"net/http"
	"time"
)

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// SecurityHeadersMiddleware aggiunge header di sicurezza
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("X-Content-Type-Options", "nosniff")
        w.Header().Set("X-Frame-Options", "DENY")
        w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
        w.Header().Set("X-XSS-Protection", "1; mode=block")
		// CSP: permette script e stili inline (necessario per il tuo layout)
        w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'")
        if config.TLSCertFile != "" && config.TLSKeyFile != "" {
            w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")
        }
        w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
        next.ServeHTTP(w, r)
    })
}

// LoggingMiddleware logga le richieste HTTP
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lw := &responseWriter{w, http.StatusOK}
		next.ServeHTTP(lw, r)
		log.Printf("%s %s %s %d %s", r.RemoteAddr, r.Method, r.URL.Path, lw.status, time.Since(start))
	})
}
