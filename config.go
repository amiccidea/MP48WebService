package main

import (
	"encoding/json"
	"log"
	"os"
)

type LogCategory struct {
	Name        string   `json:"name"`
	Directories []string `json:"directories"`
}
type FTPConfig struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	RemoteDir string `json:"remote_dir"`
	Passive   bool   `json:"passive"`
}

type TelnetConfig struct {
	Host          string `json:"host"`
	Port          int    `json:"port"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	RebootCommand string `json:"reboot_command"`
	SudoPassword  string `json:"sudo_password"` // password per sudo (opzionale)
	TimeoutSec    int    `json:"timeout_seconds"`
}

type RemoteMachine struct {
	ID     string       `json:"id"`
	Name   string       `json:"name"`
	FTP    FTPConfig    `json:"ftp"`
	Telnet TelnetConfig `json:"telnet"`
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
	AuditLogDir             string          `json:"audit_log_dir"`
	CurrentConfigurationDir string          `json:"current_configuration_dir"`
	CfWebFile               string          `json:"cf_web_file"`
	PointsCmd               string          `json:"points_cmd"`
	NetworkInterfacesFile   string          `json:"network_interfaces_file"`
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
}
