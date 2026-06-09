package main

import (
	"bufio"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// InterfaceInfo contiene i dati di una singola interfaccia
type InterfaceInfo struct {
	Name    string
	Address string
	Netmask string
	Gateway string
}

// MachineInfo contiene le informazioni lette da info.txt e version.txt
type MachineInfo struct {
	// Da info.txt
	DeviceType      string
	MP48Number      string
	ConfigName      string
	ConfigDate      string
	ConfiguratorVer string
	Operator        string
	// Da version.txt (mappa)
	Firmware map[string]string
}

// GetNetworkInterfaces legge il file di configurazione delle interfacce
func GetNetworkInterfaces() ([]InterfaceInfo, error) {
	if config.NetworkInterfacesFile == "" {
		return []InterfaceInfo{}, nil
	}
	file, err := os.Open(config.NetworkInterfacesFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var interfaces []InterfaceInfo
	var current InterfaceInfo
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "auto", "iface":
			if len(fields) >= 4 && (fields[1] == "eth0" || fields[1] == "eth1" || fields[1] == "eth2") {
				current = InterfaceInfo{Name: fields[1]}
			}
		case "address":
			current.Address = fields[1]
		case "netmask":
			current.Netmask = fields[1]
		case "gateway":
			current.Gateway = fields[1]
			if current.Name != "" {
				interfaces = append(interfaces, current)
				current = InterfaceInfo{}
			}
		}
	}
	if current.Name != "" && current.Address != "" {
		interfaces = append(interfaces, current)
	}
	return interfaces, scanner.Err()
}

// GetMachineInfo legge i file nella directory config.InfoVersionDescDir
func GetMachineInfo() (*MachineInfo, error) {
	if config.InfoVersionDescDir == "" {
		return nil, nil
	}
	info := &MachineInfo{Firmware: make(map[string]string)}
	// Legge info.txt
	infoPath := filepath.Join(config.InfoVersionDescDir, "info.txt")
	if err := parseInfoFile(infoPath, info); err != nil {
		log.Printf("Errore lettura info.txt: %v", err)
	}
	// Legge version.txt
	versionPath := filepath.Join(config.InfoVersionDescDir, "version.txt")
	if err := parseVersionFile(versionPath, info); err != nil {
		log.Printf("Errore lettura version.txt: %v", err)
	}
	return info, nil
}

func parseInfoFile(path string, info *MachineInfo) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "---") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "DEVICE TYPE:"):
			info.DeviceType = strings.TrimSpace(strings.TrimPrefix(line, "DEVICE TYPE:"))
		case strings.HasPrefix(line, "MP48 NUMBER:"):
			info.MP48Number = strings.TrimSpace(strings.TrimPrefix(line, "MP48 NUMBER:"))
		case strings.HasPrefix(line, "configuration name:"):
			info.ConfigName = strings.TrimSpace(strings.TrimPrefix(line, "configuration name:"))
		case strings.HasPrefix(line, "configuration date:"):
			info.ConfigDate = strings.TrimSpace(strings.TrimPrefix(line, "configuration date:"))
		case strings.HasPrefix(line, "configurator version:"):
			info.ConfiguratorVer = strings.TrimSpace(strings.TrimPrefix(line, "configurator version:"))
		case strings.HasPrefix(line, "operator:"):
			info.Operator = strings.TrimSpace(strings.TrimPrefix(line, "operator:"))
		}
	}
	return scanner.Err()
}

func parseVersionFile(path string, info *MachineInfo) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "---") {
			continue
		}
		if strings.Contains(line, "version:") {
			parts := strings.SplitN(line, "version:", 2)
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			info.Firmware[key] = val
		}
	}
	return scanner.Err()
}

func machineStatusHandler(w http.ResponseWriter, r *http.Request) {
	username, isAdmin := getUserContext(r)
	perms := getUserPermissions(username)

	interfaces, err := GetNetworkInterfaces()
	if err != nil {
		log.Printf("Errore lettura interfacce: %v", err)
		interfaces = []InterfaceInfo{}
	}

	machineInfo, err := GetMachineInfo()
	if err != nil {
		log.Printf("Errore lettura info macchina: %v", err)
		machineInfo = nil
	}

	data := struct {
		Username        string
		IsAdmin         bool
		Title           string
		ContentTemplate string
		Status          string
		Permissions     map[string]bool
		Interfaces      []InterfaceInfo
		MachineInfo     *MachineInfo // <-- aggiunto
	}{
		Username:        username,
		IsAdmin:         isAdmin,
		Title:           "Info CPUs",
		ContentTemplate: "machineStatusContent",
		Status:          "Informazioni sulla macchina e stato delle interfacce di rete",
		Permissions:     perms,
		Interfaces:      interfaces,
		MachineInfo:     machineInfo, // <-- aggiunto
	}
	tmpl.ExecuteTemplate(w, "layout.html", data)
}
