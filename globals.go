package main

import (
	"html/template"
	"sync"

	"github.com/gorilla/sessions"
)

// Config (definita in config.go)
var config Config

// User management
var (
	users      = make(map[string]*User)
	settings   = &AppSettings{PasswordExpiryDays: 180}
	userMutex  sync.RWMutex
	rolesMutex sync.RWMutex
)

// Session store and templates
var (
	store *sessions.CookieStore
	tmpl  *template.Template
)

// Persistence
var encryptionKey []byte
var currentDataDir string
