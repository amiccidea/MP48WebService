package main

import "time"

type UserRole string

const (
	RoleAdmin UserRole = "admin"
	RoleUser  UserRole = "user"
)

type User struct {
	ID                  string
	PasswordHash        string
	PasswordHistory     []string
	Role                UserRole
	MustChangePwd       bool
	PasswordChangedAt   time.Time
	Enabled             bool
	LastModified        time.Time
	FailedLoginAttempts int       // numero di tentativi falliti consecutivi
	LockedUntil         time.Time // timestamp di sblocco (zero = non bloccato)
}

type AppSettings struct {
	PasswordExpiryDays int
}

type Permission string

type Role struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Permissions map[string]bool `json:"permissions"`
}

type LogFileInfo struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	Size        string `json:"size"`
	ModTime     string `json:"mod_time"`
	ModTimeUnix int64  `json:"-"`
	Category    string `json:"category"`
	Directory   string `json:"directory"`
}

// In models.go
type RemoteCredential struct {
	FTPUsername    string `json:"ftp_username"`
	FTPPassword    string `json:"ftp_password"`
	TelnetUsername string `json:"telnet_username"`
	TelnetPassword string `json:"telnet_password"`
	SudoPassword   string `json:"sudo_password"`
}
type RemoteCredentials struct {
	Machines map[string]RemoteCredential `json:"machines"` // chiave: ID macchina (es. "cpu2")
}
