package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
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

// Estrae i campi da una riga di output del comando (o da file)
// Formato atteso: tipo idx ... (dipende dal tipo)
func parsePointLine(tokens []string) (pointType string, mdbIdx int, data map[string]string) {
	if len(tokens) < 3 {
		return "", 0, nil
	}
	pointType = tokens[0]
	idx, _ := strconv.Atoi(tokens[1])
	mdbIdx = idx
	data = make(map[string]string)
	switch pointType {
	case "SGN":
		// Formato: SGN idx len val quality tiv timestamp time
		if len(tokens) >= 7 {
			data["value"] = tokens[3]
			data["quality"] = tokens[4] // BL,SB,NT,IV
			data["tiv"] = tokens[5]
			data["timestamp"] = tokens[6] + " " + tokens[7]
		}
	case "MSR":
		// MSR idx len val fval quality tiv timestamp time
		if len(tokens) >= 8 {
			data["value"] = tokens[4]
			data["quality"] = tokens[6] // OV,BL,SB,NT,IV
			data["tiv"] = tokens[7]
			data["timestamp"] = tokens[8] + " " + tokens[9]
		}
	case "CTR":
		// CTR idx val quality rem timestamp time
		if len(tokens) >= 6 {
			data["value"] = tokens[2]
			data["quality"] = tokens[3] // QU,SE,Len
			data["rem"] = tokens[4]
			data["timestamp"] = tokens[5] + " " + tokens[6]
		}
	case "STP":
		// STP idx val quality rem timestamp time
		if len(tokens) >= 6 {
			data["value"] = tokens[2]
			data["quality"] = tokens[3] // QU,SE
			data["rem"] = tokens[4]
			data["timestamp"] = tokens[5] + " " + tokens[6]
		}
	}
	return
}

// getPointsOutput restituisce l'output dei punti (da comando o da file)
func getPointsOutput() (string, error) {
	// Su Windows, se esiste un file points.txt (generato periodicamente), leggilo
	// Altrimenti restituisci stringa vuota (dati default)
	if runtime.GOOS == "windows" {
		pointsFile := "points.txt"
		if _, err := os.Stat(pointsFile); err == nil {
			data, err := os.ReadFile(pointsFile)
			if err == nil {
				return string(data), nil
			}
		}
		// Se non esiste, restituisci output vuoto (verranno usati valori default)
		return "", nil
	}

	// Su Linux, esegui il comando reale
	cmd := exec.Command(config.PointsCmd)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	return out.String(), nil
}

// readCfWebLines legge il file di configurazione dei segnali
func readCfWebLines() ([]string, error) {
	if config.CfWebFile == "" {
		return nil, fmt.Errorf("cf_web_file non configurato")
	}
	f, err := os.Open(config.CfWebFile)
	if err != nil {
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
	return lines, scanner.Err()
}

// parseCfWebLine analizza una riga del file cf_web.txt
func parseCfWebLine(line string) (typeName string, mdbIdx int, desc string, err error) {
	parts := strings.SplitN(line, "#", 2)
	if len(parts) == 2 {
		desc = strings.TrimSpace(parts[1])
	}
	fields := strings.Fields(parts[0])
	if len(fields) < 2 {
		err = fmt.Errorf("formato riga errato: %s", line)
		return
	}
	typeName = fields[0]
	mdbIdx, err = strconv.Atoi(fields[1])
	return
}

// GetSignalsData è la funzione principale
func GetSignalsData() (*SignalsData, error) {
	// Leggi il file cf_web.txt
	cfLines, err := readCfWebLines()
	if err != nil {
		// Se il file non esiste, ritorna dati vuoti
		return &SignalsData{}, nil
	}

	// Ottieni i valori live (se disponibili)
	pointsOutput, _ := getPointsOutput()
	pointsMap := make(map[int]map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(pointsOutput))
	for scanner.Scan() {
		tokens := strings.Fields(scanner.Text())
		if len(tokens) < 2 {
			continue
		}
		typ, idx, data := parsePointLine(tokens)
		if typ != "" && idx > 0 && data != nil {
			pointsMap[idx] = data
		}
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
		typ, idx, desc, err := parseCfWebLine(line)
		if err != nil {
			continue
		}
		pointInfo, ok := pointsMap[idx]
		value := ""
		timestamp := ""
		quality := ""
		if ok {
			value = pointInfo["value"]
			timestamp = pointInfo["timestamp"]
			quality = pointInfo["quality"]
		} else {
			// Valore di default: '?'
			value = "?"
			timestamp = ""
			quality = ""
		}
		sp := SignalPoint{
			MdbIdx:    idx,
			Value:     value,
			Timestamp: timestamp,
			Desc:      desc,
		}
		// Aggiungi qualità se volessi usarla
		if quality != "" {
			sp.Quality = make(map[string]string)
			// Puoi splittare i caratteri
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
		}
	}
	return data, nil
}
