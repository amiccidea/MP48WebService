package main

import (
	"net/http"
	"sync"
	"time"
)

// RateLimiter tiene traccia dei tentativi per IP
type RateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*Visitor
	limit    int           // numero massimo di richieste
	window   time.Duration // finestra di tempo
}

// Visitor rappresenta un singolo IP
type Visitor struct {
	mu       sync.Mutex
	count    int
	lastSeen time.Time
}

// NewRateLimiter crea un nuovo rate limiter
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		visitors: make(map[string]*Visitor),
		limit:    limit,
		window:   window,
	}
	// Avvia una goroutine per pulire i visitor scaduti
	go rl.cleanup()
	return rl
}

// Allow controlla se l'IP può fare una richiesta
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, exists := rl.visitors[ip]
	if !exists {
		rl.visitors[ip] = &Visitor{
			count:    1,
			lastSeen: time.Now(),
		}
		return true
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	// Se è passata più della finestra, resetta il contatore
	if time.Since(v.lastSeen) > rl.window {
		v.count = 1
		v.lastSeen = time.Now()
		return true
	}

	v.lastSeen = time.Now()
	if v.count >= rl.limit {
		return false
	}
	v.count++
	return true
}

// cleanup rimuove i visitor inattivi per liberare memoria
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		for ip, v := range rl.visitors {
			v.mu.Lock()
			if time.Since(v.lastSeen) > rl.window*2 {
				delete(rl.visitors, ip)
			}
			v.mu.Unlock()
		}
		rl.mu.Unlock()
	}
}

// RateLimitMiddleware applica il rate limiting
func RateLimitMiddleware(limiter *RateLimiter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := getClientIP(r)
		if !limiter.Allow(ip) {
			http.Error(w, "Troppe richieste, riprova più tardi", http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}

// getClientIP estrae l'IP del client (considera proxy)
func getClientIP(r *http.Request) string {
	// Prova a leggere da X-Forwarded-For (se dietro proxy)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	return r.RemoteAddr
}
