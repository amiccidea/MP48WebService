package main

import (
	"bufio"
	"fmt"
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

// CPUInterfaces associa un numero CPU alle sue interfacce
type CPUInterfaces struct {
	CPU        int
	IsLocal    bool
	Interfaces []InterfaceInfo
}

// MachineInfo contiene le informazioni lette da info.txt e version.txt
type MachineInfo struct {
	DeviceType      string
	MP48Number      string
	ConfigName      string
	ConfigDate      string
	ConfiguratorVer string
	Operator        string
	Firmware        map[string]string
}

// ==================== FUNZIONI PER COMPATIBILITÀ (config.go) ====================

// GetNetworkInterfaces legge il file di configurazione delle interfacce (singolo file)
// Usata da config.go per ottenere l'IP locale
func GetNetworkInterfaces() ([]InterfaceInfo, error) {
	if config.NetworkInterfacesFile == "" {
		return []InterfaceInfo{}, nil
	}
	// Usa il nuovo parser per consistenza
	return ParseInterfaceFile(config.NetworkInterfacesFile)
}

// ==================== NUOVO PARSER ROBUSTO ====================

// ParseInterfaceFile estrae le interfacce da un file usando una mappa per nome
// Così funziona anche se manca "gateway" (es. eth0)
func ParseInterfaceFile(filePath string) ([]InterfaceInfo, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Mappa per accumulare le interfacce per nome
	ifaceMap := make(map[string]*InterfaceInfo)
	var ifaceOrder []string // per mantenere l'ordine

	scanner := bufio.NewScanner(file)
	var currentName string

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
		case "iface":
			if len(fields) >= 2 {
				currentName = fields[1]
				if _, ok := ifaceMap[currentName]; !ok {
					ifaceMap[currentName] = &InterfaceInfo{Name: currentName}
					ifaceOrder = append(ifaceOrder, currentName)
				}
			}
		case "address":
			if currentName != "" && len(fields) >= 2 {
				ifaceMap[currentName].Address = fields[1]
			}
		case "netmask":
			if currentName != "" && len(fields) >= 2 {
				ifaceMap[currentName].Netmask = fields[1]
			}
		case "gateway":
			if currentName != "" && len(fields) >= 2 {
				ifaceMap[currentName].Gateway = fields[1]
			}
		}
	}

	// Converti la mappa in slice, solo le interfacce con indirizzo
	var interfaces []InterfaceInfo
	for _, name := range ifaceOrder {
		if ifaceMap[name].Address != "" {
			interfaces = append(interfaces, *ifaceMap[name])
		}
	}
	return interfaces, scanner.Err()
}

// GetAllNetworkInterfaces legge tutti i file interfaces_%d e restituisce le interfacce per CPU
func GetAllNetworkInterfaces() ([]CPUInterfaces, error) {
	if config.RemoteInterfacesPattern == "" {
		// Fallback: usa il file singolo
		ifaces, err := GetNetworkInterfaces()
		if err != nil {
			return nil, err
		}
		return []CPUInterfaces{{CPU: 1, IsLocal: true, Interfaces: ifaces}}, nil
	}

	// Ottieni l'IP locale dal file delle interfacce
	localIP, err := GetLocalIPFromFile()
	if err != nil {
		localIP = ""
	}

	var allCPU []CPUInterfaces
	maxCPU := 10
	for cpuNum := 1; cpuNum <= maxCPU; cpuNum++ {
		filePath := fmt.Sprintf(config.RemoteInterfacesPattern, cpuNum)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			continue
		}
		ifaces, err := ParseInterfaceFile(filePath)
		if err != nil {
			log.Printf("Errore lettura %s: %v", filePath, err)
			continue
		}

		// Verifica se questa CPU ha l'IP locale
		isLocal := false
		for _, iface := range ifaces {
			if iface.Address == localIP {
				isLocal = true
				break
			}
		}

		allCPU = append(allCPU, CPUInterfaces{
			CPU:        cpuNum,
			IsLocal:    isLocal,
			Interfaces: ifaces,
		})
	}
	return allCPU, nil
}

// ==================== INFO MACCHINA ====================

// GetMachineInfo legge i file nella directory config.InfoVersionDescDir
func GetMachineInfo() (*MachineInfo, error) {
	if config.InfoVersionDescDir == "" {
		return nil, nil
	}
	info := &MachineInfo{Firmware: make(map[string]string)}
	infoPath := filepath.Join(config.InfoVersionDescDir, "info.txt")
	if err := parseInfoFile(infoPath, info); err != nil {
		log.Printf("Errore lettura info.txt: %v", err)
	}
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

// ==================== HANDLER ====================

func machineStatusHandler(w http.ResponseWriter, r *http.Request) {
	username, isAdmin := getUserContext(r)
	perms := getUserPermissions(username)

	cpuInterfaces, err := GetAllNetworkInterfaces()
	if err != nil {
		log.Printf("Errore lettura interfacce: %v", err)
		cpuInterfaces = []CPUInterfaces{}
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
		CPUInterfaces   []CPUInterfaces
		MachineInfo     *MachineInfo
		IsMultiCPU      bool
	}{
		Username:        username,
		IsAdmin:         isAdmin,
		Title:           "Info CPUs",
		ContentTemplate: "machineStatusContent",
		Status:          "Informazioni sulla macchina e stato delle interfacce di rete",
		Permissions:     perms,
		CPUInterfaces:   cpuInterfaces,
		MachineInfo:     machineInfo,
		IsMultiCPU:      isMultiCPU(),
	}
	tmpl.ExecuteTemplate(w, "layout.html", data)
}
