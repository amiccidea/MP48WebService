package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// GetLocalIPFromFile restituisce il primo indirizzo IPv4 dal file delle interfacce locali
func GetLocalIPFromFile() (string, error) {
	if config.NetworkInterfacesFile == "" {
		return "", nil
	}
	f, err := os.Open(config.NetworkInterfacesFile)
	if err != nil {
		return "", err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	var ip string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "address" {
			ip = fields[1]
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return ip, nil
}

// GetRemoteIPsFromInterfaces legge i file interfaces_<num> per le macchine remote
// utilizzando il pattern config.RemoteInterfacesPattern
func GetRemoteIPsFromInterfaces() (map[string]string, error) {
	if config.RemoteInterfacesPattern == "" {
		// Fallback: se non è definito, proviamo a dedurre
		return nil, nil
	}
	ipMap := make(map[string]string)
	maxCPUs := 10
	for cpuNum := 1; cpuNum <= maxCPUs; cpuNum++ {
		filePath := fmt.Sprintf(config.RemoteInterfacesPattern, cpuNum)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			// Se il file non esiste, interrompi (assumiamo CPU consecutive)
			break
		}
		ip, err := getIPFromInterfacesFile(filePath)
		if err != nil {
			continue
		}
		if ip != "" {
			id := fmt.Sprintf("cpu%d", cpuNum)
			ipMap[id] = ip
		}
	}
	return ipMap, nil
}

func getIPFromInterfacesFile(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	var ip string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "address" {
			ip = fields[1]
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return ip, nil
}

// ResolveRemoteMachines risolve gli IP delle macchine remote
func ResolveRemoteMachines() error {
	// Se non c'è un pattern remoto definito, non fare nulla
	if config.RemoteInterfacesPattern == "" {
		return nil
	}
	ipMap, err := GetRemoteIPsFromInterfaces()
	if err != nil {
		return err
	}
	for i := range config.RemoteMachines {
		if config.RemoteMachines[i].ID == "local" {
			continue
		}
		if ip, ok := ipMap[config.RemoteMachines[i].ID]; ok {
			config.RemoteMachines[i].Host = ip
		} else {
			config.RemoteMachines[i].Host = "NON TROVATO"
		}
	}
	return nil
}
