package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

type SignalPoint struct {
	MdbIdx    int
	Value     string
	Length    int
	Quality   map[string]string
	Timestamp string
	Tiv       string
	Desc      string
}

type SignalsData struct {
	Positions []SignalPoint
	Measures  []SignalPoint
	Commands  []SignalPoint
	Setpoints []SignalPoint
	Warnings  []SignalPoint
	Alarms    []SignalPoint
}

// parsePointLinePipe gestisce il formato con pipe (sia file che console)
func parsePointLinePipe(line string) (pointType string, mdbIdx int, data map[string]string) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" ||
		strings.HasPrefix(trimmed, "----") ||
		strings.HasPrefix(trimmed, "TYPE") ||
		strings.HasPrefix(trimmed, "Itaco") ||
		strings.HasPrefix(trimmed, "Shared") ||
		strings.HasPrefix(trimmed, "---------------------------------------------------------------------------------------------------------") ||
		strings.HasPrefix(trimmed, " IOADDR") ||
		strings.HasPrefix(trimmed, "   IOADDR") ||
		strings.HasPrefix(trimmed, " [") ||
		strings.Contains(trimmed, "------------") {
		return "", 0, nil
	}

	parts := strings.Split(line, "|")
	if len(parts) < 4 {
		return "", 0, nil
	}

	typeField := strings.TrimSpace(parts[0])
	idxField := strings.TrimSpace(parts[1])
	lenOrValField := strings.TrimSpace(parts[2])
	valOrQualityField := strings.TrimSpace(parts[3])

	if typeField == "" || idxField == "" {
		return "", 0, nil
	}

	switch typeField {
	case "SIGNAL":
		typeField = "SGN"
	case "MEASURE":
		typeField = "MSR"
	case "CONTROL":
		typeField = "CTR"
	case "SETPOINT":
		typeField = "STP"
	case "VARIABLE":
		return "", 0, nil
	}

	if typeField != "SGN" && typeField != "MSR" && typeField != "CTR" && typeField != "STP" {
		return "", 0, nil
	}

	idx, err := strconv.Atoi(idxField)
	if err != nil {
		return "", 0, nil
	}

	data = make(map[string]string)
	data["len"] = lenOrValField
	data["value"] = valOrQualityField

	if len(parts) >= 5 {
		qualityField := strings.TrimSpace(parts[4])
		if strings.Contains(qualityField, ",") {
			data["quality"] = qualityField
		}
	}
	if len(parts) >= 6 {
		tivField := strings.TrimSpace(parts[5])
		if tivField != "" && strings.Contains(tivField, "time:") {
			tsParts := strings.SplitN(tivField, "time:", 2)
			if len(tsParts) == 2 {
				data["timestamp"] = strings.TrimSpace(tsParts[1])
			}
		} else {
			data["tiv"] = tivField
		}
	}
	if len(parts) >= 7 {
		ts := strings.TrimSpace(parts[6])
		if ts != "" && ts != "|" && !strings.Contains(ts, "----") {
			if strings.Contains(ts, "time:") {
				tsParts := strings.SplitN(ts, "time:", 2)
				if len(tsParts) == 2 {
					data["timestamp"] = strings.TrimSpace(tsParts[1])
				}
			} else {
				data["timestamp"] = ts
			}
		}
	}
	if timestamp, ok := data["timestamp"]; !ok || timestamp == "" {
		if len(parts) >= 5 {
			ts := strings.TrimSpace(parts[4])
			if strings.Contains(ts, "time:") {
				tsParts := strings.SplitN(ts, "time:", 2)
				if len(tsParts) == 2 {
					data["timestamp"] = strings.TrimSpace(tsParts[1])
				}
			}
		}
	}

	return typeField, idx, data
}

// getPointsOutput esegue lo script wrapper e legge il file points.txt
func getPointsOutput() (string, error) {
	log.Printf("🔍 getPointsOutput: avvio su OS=%s, Mp48Type=%s", runtime.GOOS, config.Mp48Type)

	filePath := config.PointsFile
	if filePath == "" {
		filePath = "points.txt"
	}

	scriptPath := config.PointsCmd
	if scriptPath == "" {
		log.Printf("⚠️ PointsCmd non configurato, provo a leggere points.txt esistente")
		data, err := ioutil.ReadFile(filePath)
		if err == nil {
			log.Printf("✅ points.txt letto (%d byte)", len(data))
			return string(data), nil
		}
		return "", nil
	}

	scriptPath = strings.TrimSpace(scriptPath)

	// Definiamo i parametri crudi per il Kernel
	binaryPath := "/bin/bash"
	argv := []string{"bash", scriptPath}
	envv := []string{"PATH=/bin:/usr/bin:/sbin:/usr/sbin"}

	// Lancio della syscall atomica
	pid, err := InvocatoreSyscallNativo(binaryPath, argv, envv)
	if err != nil {
		log.Printf("❌ Chiamata hardware fallita: %v", err)
	} else {
		log.Printf("✅ Processo clonato con successo! PID generato: %d", pid)
		
		// Blocchiamo il padre in modo sincrono finché lo script non ha finito di scrivere in /tmp
		var wstatus syscall.WaitStatus
		_, _ = syscall.Wait4(pid, &wstatus, 0, nil)
		log.Printf("Processo figlio terminato con codice: %d", wstatus.ExitStatus())
	}
	// Leggi il file generato dallo script
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		log.Printf("❌ Errore lettura %s: %v", filePath, err)
		return "", nil
	}
	log.Printf("✅ points.txt letto (%d byte)", len(data))
	return string(data), nil
}

// readCfWebLines legge il file di configurazione dei segnali
func readCfWebLines() ([]string, error) {
	if config.CfWebFile == "" {
		log.Printf("❌ cf_web_file non configurato")
		return nil, fmt.Errorf("cf_web_file non configurato")
	}
	log.Printf("📂 Leggo cf_web.txt da: %s", config.CfWebFile)
	f, err := os.Open(config.CfWebFile)
	if err != nil {
		log.Printf("❌ Errore apertura %s: %v", config.CfWebFile, err)
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("❌ Errore scansione %s: %v", config.CfWebFile, err)
		return nil, err
	}

	log.Printf("✅ Lette %d righe non commentate", len(lines))
	return lines, nil
}

// parseCfWebLine analizza una riga del file cf_web.txt
func parseCfWebLine(line string) (typeName string, mdbIdx int, defaultVal string, desc string, err error) {
	parts := strings.SplitN(line, "#", 2)
	if len(parts) == 2 {
		desc = strings.TrimSpace(parts[1])
	}
	fields := strings.Fields(parts[0])
	if len(fields) < 2 {
		err = fmt.Errorf("formato riga errato: %s (servono almeno 2 campi)", line)
		return
	}
	typeName = fields[0]
	mdbIdx, err = strconv.Atoi(fields[1])
	if err != nil {
		return
	}
	if len(fields) >= 3 {
		defaultVal = fields[2]
	} else {
		defaultVal = "?"
	}
	return
}

// GetSignalsData è la funzione principale
func GetSignalsData() (*SignalsData, error) {
	log.Printf("🚀 GetSignalsData: inizio")

	cfLines, err := readCfWebLines()
	if err != nil {
		log.Printf("⚠️ readCfWebLines fallito: %v, restituisco dati vuoti", err)
		return &SignalsData{}, nil
	}
	if len(cfLines) == 0 {
		log.Printf("⚠️ Nessuna riga valida in cf_web.txt, restituisco dati vuoti")
		return &SignalsData{}, nil
	}
	log.Printf("📋 cfLines: %d righe", len(cfLines))

	pointsOutput, err := getPointsOutput()
	if err != nil {
		log.Printf("⚠️ getPointsOutput fallito: %v, continuo con valori vuoti", err)
		pointsOutput = ""
	}
	log.Printf("📊 pointsOutput lunghezza: %d byte", len(pointsOutput))

	pointsMap := make(map[int]map[string]string)
	if pointsOutput != "" {
		scanner := bufio.NewScanner(strings.NewReader(pointsOutput))
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			typ, idx, data := parsePointLinePipe(line)
			if typ != "" && idx > 0 && data != nil {
				pointsMap[idx] = data
			}
		}
		if err := scanner.Err(); err != nil {
			log.Printf("⚠️ Errore scanner: %v", err)
		}
		log.Printf("✅ Parsing output: %d punti trovati su %d righe", len(pointsMap), lineNum)
	} else {
		log.Printf("⚠️ pointsOutput vuoto")
	}

	data := &SignalsData{
		Positions: []SignalPoint{},
		Measures:  []SignalPoint{},
		Commands:  []SignalPoint{},
		Setpoints: []SignalPoint{},
		Warnings:  []SignalPoint{},
		Alarms:    []SignalPoint{},
	}

	for _, line := range cfLines {
		typ, idx, defVal, desc, err := parseCfWebLine(line)
		if err != nil {
			log.Printf("⚠️ Errore parsing riga '%s': %v", line, err)
			continue
		}
		pointInfo, ok := pointsMap[idx]
		value := defVal
		timestamp := ""
		if ok {
			if v, exists := pointInfo["value"]; exists {
				value = v
			}
			if ts, exists := pointInfo["timestamp"]; exists {
				timestamp = ts
			}
		}
		sp := SignalPoint{
			MdbIdx:    idx,
			Value:     value,
			Timestamp: timestamp,
			Desc:      desc,
		}
		if ok {
			if _, exists := pointInfo["quality"]; exists {
				sp.Quality = make(map[string]string)
			}
		}

		switch typ {
		case "SGN":
			data.Positions = append(data.Positions, sp)
		case "MSR":
			data.Measures = append(data.Measures, sp)
		case "CTR":
			data.Commands = append(data.Commands, sp)
		case "SPR":
			data.Setpoints = append(data.Setpoints, sp)
		case "WARN":
			data.Warnings = append(data.Warnings, sp)
		case "ALM":
			data.Alarms = append(data.Alarms, sp)
		default:
			log.Printf("⚠️ Tipo sconosciuto '%s' per idx %d", typ, idx)
		}
	}

	log.Printf("✅ GetSignalsData completato: Posizioni=%d, Misure=%d, Comandi=%d, Setpoint=%d, Warning=%d, Allarmi=%d",
		len(data.Positions), len(data.Measures), len(data.Commands), len(data.Setpoints),
		len(data.Warnings), len(data.Alarms))

	return data, nil
}