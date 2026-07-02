package main

import (
	"encoding/json"
	"log"
	"os"
)

var remoteCreds *RemoteCredentials

type LogCategory struct {
	Name        string   `json:"name"`
	Directories []string `json:"directories"`
}
type FTPConfig struct {
	Port      int    `json:"port"`
	RemoteDir string `json:"remote_dir"`
	Passive   bool   `json:"passive"`
	Username  string `json:"-"` // non serializzato, popolato da remote_creds
	Password  string `json:"-"`
}

type TelnetConfig struct {
	Port          int    `json:"port"`
	RebootCommand string `json:"reboot_command"`
	TimeoutSec    int    `json:"timeout_seconds"`
	Username      string `json:"-"`
	Password      string `json:"-"`
	SudoPassword  string `json:"-"`
}

type RemoteMachine struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	Host           string       `json:"-"` // risolto da interfaces
	FTPUsername    string       `json:"-"` // caricato da remote_creds
	FTPPassword    string       `json:"-"`
	TelnetUsername string       `json:"-"`
	TelnetPassword string       `json:"-"`
	SudoPassword   string       `json:"-"`
	FTP            FTPConfig    `json:"ftp"`
	Telnet         TelnetConfig `json:"telnet"`
}
type RemoteCPUConfig struct {
	FTP    FTPConfig    `json:"ftp"`
	Telnet TelnetConfig `json:"telnet"`
}

type Config struct {
	LDAPServer          string   `json:"ldap_server"`
	BaseDN              string   `json:"base_dn"`
	BindDN              string   `json:"bind_dn"`
	BindPassword        string   `json:"bind_password"`
	UserSearchBase      string   `json:"user_search_base"`
	UserFilter          string   `json:"user_filter"`
	AdminUsers          []string `json:"admin_users"`
	SessionSecret       string   `json:"session_secret"`
	SessionMaxAgeSecond int      `json:"session_max_age_seconds"`
	// ... campi esistenti ...
	SessionInactivityMinutes int `json:"session_inactivity_minutes"`
	// opzionale, se vuoi anche timeout assoluto:
	SessionAbsoluteHours    int             `json:"session_absolute_hours"`
	PasswordExpiryDays      int             `json:"password_expiry_days"` // <-- NUOVO
	Port                    string          `json:"port"`
	PortSSL                 string          `json:"portssl"`
	TLSCertFile             string          `json:"tls_cert_file"`
	TLSKeyFile              string          `json:"tls_key_file"`
	LogFilePath             string          `json:"log_file_path"`
	ConfigHistoryDir        string          `json:"config_history_dir"`
	ConfigExtensions        []string        `json:"config_extensions"`
	UploadDir               string          `json:"upload_dir"`
	LogExtensions           []string        `json:"log_extensions"`
	LogCategories           []LogCategory   `json:"log_categories"`
	ProtectedUsers          []string        `json:"protected_users"`
	DataDir                 string          `json:"data_dir"`
	EncryptionKeyPath       string          `json:"encryption_key_path"`
	AuditLogDir             string          `json:"audit_log_dir"`
	CurrentConfigurationDir string          `json:"current_configuration_dir"`
	CfWebFile               string          `json:"cf_web_file"`
	PointsCmd               string          `json:"points_cmd"`
	NetworkInterfacesFile   string          `json:"network_interfaces_file"`
	RemoteInterfacesPattern string          `json:"remote_interfaces_pattern"`
	AnalogInputsCmd         string          `json:"analog_inputs_cmd"`
	AnalogInputsDescFile    string          `json:"analog_inputs_desc_file"`
	AnalogInputsDescBase    string          `json:"analog_inputs_desc_base"`
	InfoVersionDescDir      string          `json:"info_version_desc_file"`
	Mp48Type                string          `json:"mp48_type"`
	ExtensionFilesConfig    []string        `json:"extension_files_config"`
	RemoteMachines          []RemoteMachine `json:"remote_machines"`
}

func initConfig() {
	data, err := os.ReadFile("config.json")
	if err != nil {
		log.Fatal("Errore lettura config.json:", err)
	}
	if err := json.Unmarshal(data, &config); err != nil {
		log.Fatal("Errore parsing config.json:", err)
	}
	// Inizializza le impostazioni dal config
	settings.PasswordExpiryDays = config.PasswordExpiryDays
	if settings.PasswordExpiryDays == 0 {
		settings.PasswordExpiryDays = 180 // default
	}
	// Inizializza currentDataDir PRIMA di usarlo
	currentDataDir = config.DataDir
	if currentDataDir == "" {
		currentDataDir = "./data"
	}
	// Crea la directory se non esiste
	if err := os.MkdirAll(currentDataDir, 0700); err != nil {
		log.Printf("Errore creazione directory dati: %v", err)
	}

	// 1. Risolvi IP delle macchine remote (file interfaces_%d)
	if err := ResolveRemoteMachines(); err != nil {
		log.Printf("Errore risoluzione IP remoti: %v", err)
	}

	// 2. Aggiungi la macchina locale (se non già presente)
	foundLocal := false
	for _, m := range config.RemoteMachines {
		if m.ID == "local" {
			foundLocal = true
			break
		}
	}
	if !foundLocal {
		localIP := getLocalIP()
		localMachine := RemoteMachine{
			ID:   "local",
			Name: "Questa macchina (locale)",
			Host: localIP,
			FTP: FTPConfig{
				Port:      21,
				RemoteDir: "/backup",
				Passive:   true,
			},
			Telnet: TelnetConfig{
				Port:          23,
				RebootCommand: "sudo reboot",
				TimeoutSec:    10,
			},
		}
		config.RemoteMachines = append(config.RemoteMachines, localMachine)
		log.Printf("Macchina locale aggiunta con IP: %s", localIP)
	}

	// 3. Carica credenziali remote (se presenti) e applicale
	remoteCreds, err := loadRemoteCredentials(currentDataDir)
	if err != nil {
		log.Printf("Errore caricamento credenziali remote: %v", err)
		remoteCreds = nil
	}

	if remoteCreds != nil && remoteCreds.Machines != nil {
		log.Printf("Credenziali caricate per %d macchine", len(remoteCreds.Machines))
		for id, cred := range remoteCreds.Machines {
			log.Printf("  %s: FTP=%s, Telnet=%s", id, cred.FTPUsername, cred.TelnetUsername)
		}
		for i := range config.RemoteMachines {
			machineID := config.RemoteMachines[i].ID
			if cred, ok := remoteCreds.Machines[machineID]; ok {
				config.RemoteMachines[i].FTP.Username = cred.FTPUsername
				config.RemoteMachines[i].FTP.Password = cred.FTPPassword
				config.RemoteMachines[i].Telnet.Username = cred.TelnetUsername
				config.RemoteMachines[i].Telnet.Password = cred.TelnetPassword
				config.RemoteMachines[i].Telnet.SudoPassword = cred.SudoPassword
			}
		}
	} else {
		log.Println("AVVISO: Nessuna credenziale remota configurata. Usare l'interfaccia admin per impostarle.")
	}
}

// getLocalIP restituisce l'IP della macchina locale (priorità eth2, poi primo IP trovato)
func getLocalIP() string {
	localIP, err := GetLocalIPFromFile()
	if err != nil || localIP == "" {
		// Fallback: usa GetNetworkInterfaces()
		interfaces, err := GetNetworkInterfaces()
		if err == nil {
			for _, iface := range interfaces {
				if iface.Address != "" {
					return iface.Address
				}
			}
		}
		return "Non disponibile"
	}
	return localIP
}
