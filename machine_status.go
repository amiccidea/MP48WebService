package main

import (
	"bufio"
	"log"
	"net/http"
	"os"
	"strings"
)

// InterfaceInfo contiene i dati di una singola interfaccia
type InterfaceInfo struct {
	Name    string
	Address string
	Netmask string
	Gateway string
}

// GetNetworkInterfaces legge il file di configurazione delle interfacce
// e restituisce una slice con le informazioni per eth0, eth1, eth2
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
			// Per semplicità, cerchiamo le righe "iface ethX inet static"
			if len(fields) >= 4 && fields[1] == "eth0" || fields[1] == "eth1" || fields[1] == "eth2" {
				current = InterfaceInfo{Name: fields[1]}
			}
		case "address":
			current.Address = fields[1]
		case "netmask":
			current.Netmask = fields[1]
		case "gateway":
			current.Gateway = fields[1]
			// Quando troviamo gateway, l'interfaccia è completa
			if current.Name != "" {
				interfaces = append(interfaces, current)
				current = InterfaceInfo{}
			}
		}
	}
	// Se l'ultima interfaccia non aveva gateway (forse solo address+netmask), aggiungila comunque
	if current.Name != "" && current.Address != "" {
		interfaces = append(interfaces, current)
	}
	return interfaces, scanner.Err()
}

func machineStatusHandler(w http.ResponseWriter, r *http.Request) {
	username, isAdmin := getUserContext(r)
	perms := getUserPermissions(username)

	// Recupera le interfacce di rete
	interfaces, err := GetNetworkInterfaces()
	if err != nil {
		log.Printf("Errore lettura interfacce di rete: %v", err)
		interfaces = []InterfaceInfo{}
	}

	data := struct {
		Username        string
		IsAdmin         bool
		Title           string
		ContentTemplate string
		Status          string
		Permissions     map[string]bool
		Interfaces      []InterfaceInfo
	}{
		Username:        username,
		IsAdmin:         isAdmin,
		Title:           "Stato Macchina",
		ContentTemplate: "machineStatusContent",
		Status:          "Macchina operativa, mirror sincronizzato",
		Permissions:     perms,
		Interfaces:      interfaces,
	}
	tmpl.ExecuteTemplate(w, "layout.html", data)
}
