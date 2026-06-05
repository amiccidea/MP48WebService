package main

import (
	"bufio"
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// AnalogInput rappresenta una singola misura analogica
type AnalogInput struct {
	IOA                int
	Value              float64
	OV, BL, SB, NT, IV int
	Timestamp          string
	Desc               string
}

type analogPointData struct {
	Value              float64
	OV, BL, SB, NT, IV int
	Timestamp          string
}

// GetAnalogInputs restituisce la lista degli ingressi analogici per una data CPU (1-4)
func GetAnalogInputs(cpuID int) ([]AnalogInput, error) {
	var ioaStart, ioaEnd int
	switch cpuID {
	case 2:
		ioaStart, ioaEnd = 250, 259
	case 3:
		ioaStart, ioaEnd = 350, 359
	case 4:
		ioaStart, ioaEnd = 450, 459
	default:
		ioaStart, ioaEnd = 150, 159
	}

	// 1. Ottieni valori live
	pointsData, err := getAnalogPointsData(ioaStart, ioaEnd)
	if err != nil {
		return nil, err
	}

	// 2. Leggi descrizioni per questa CPU
	descMap, err := readAnalogDescFile(cpuID)
	if err != nil {
		descMap = make(map[int]string)
	}

	// 3. Costruisci risultato
	var inputs []AnalogInput
	for ioa := ioaStart; ioa <= ioaEnd; ioa++ {
		data, ok := pointsData[ioa]
		if !ok {
			continue
		}
		inputs = append(inputs, AnalogInput{
			IOA:       ioa,
			Value:     data.Value,
			OV:        data.OV,
			BL:        data.BL,
			SB:        data.SB,
			NT:        data.NT,
			IV:        data.IV,
			Timestamp: data.Timestamp,
			Desc:      descMap[ioa],
		})
	}
	return inputs, nil
}

func getAnalogPointsData(ioaStart, ioaEnd int) (map[int]analogPointData, error) {
	result := make(map[int]analogPointData)

	if runtime.GOOS == "windows" {
		// Mock per test
		for ioa := ioaStart; ioa <= ioaEnd; ioa++ {
			result[ioa] = analogPointData{
				Value:     float64(ioa%100) * 1.23,
				Timestamp: time.Now().Format("2006-01-02 15:04:05"),
			}
		}
		return result, nil
	}

	// Linux: esegue rsl_smm
	cmd := exec.Command("sh", "-c", fmt.Sprintf("echo 'm\nq\n' | %s | awk -v s=%d -v e=%d -F'|' '{ if ($1 > s && $1 < e) print $1\" \"$2\" \"$3\" \"$4\" \"$5; }'", config.AnalogInputsCmd, ioaStart, ioaEnd))
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(&out)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		ioa, _ := strconv.Atoi(fields[0])
		val, _ := strconv.ParseFloat(fields[1], 64)
		qc := strings.Split(fields[2], ",")
		var ov, bl, sb, nt, iv int
		if len(qc) >= 5 {
			ov, _ = strconv.Atoi(qc[0])
			bl, _ = strconv.Atoi(qc[1])
			sb, _ = strconv.Atoi(qc[2])
			nt, _ = strconv.Atoi(qc[3])
			iv, _ = strconv.Atoi(qc[4])
		}
		timestamp := fields[4]
		result[ioa] = analogPointData{
			Value:     val,
			OV:        ov,
			BL:        bl,
			SB:        sb,
			NT:        nt,
			IV:        iv,
			Timestamp: timestamp,
		}
	}
	return result, scanner.Err()
}

// readAnalogDescFile legge il file di descrizione per la CPU specificata
func readAnalogDescFile(cpuID int) (map[int]string, error) {
	if config.AnalogInputsDescBase == "" {
		return nil, nil
	}
	path := fmt.Sprintf(config.AnalogInputsDescBase, cpuID)
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	descMap := make(map[int]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "MSR" {
			continue
		}
		ioa, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		desc := strings.Join(fields[2:], " ")
		descMap[ioa] = desc
	}
	return descMap, scanner.Err()
}

// GetAvailableCPUs restituisce la lista delle CPU per cui esiste il file di descrizione
func GetAvailableCPUs() ([]int, error) {
	if config.AnalogInputsDescFile == "" {
		log.Println("AnalogInputsDescFile vuoto, restituisco [1]")
		return []int{1}, nil
	}
	// Costruisci un pattern con * al posto di %d
	pattern := strings.Replace(config.AnalogInputsDescFile, "%d", "*", -1)
	log.Printf("Pattern glob: %s", pattern)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		log.Printf("Errore nel glob pattern %s: %v", pattern, err)
		return []int{1}, nil
	}
	log.Printf("File trovati: %v", matches)
	var cpus []int
	re := regexp.MustCompile(`\d+`)
	for _, match := range matches {
		base := filepath.Base(match)
		log.Printf("Analizzo file: %s", base)
		numStr := re.FindString(base)
		if numStr != "" {
			num, err := strconv.Atoi(numStr)
			if err == nil {
				log.Printf("Estratto numero CPU: %d", num)
				cpus = append(cpus, num)
			} else {
				log.Printf("Errore conversione %s: %v", numStr, err)
			}
		} else {
			log.Printf("Nessun numero trovato in %s", base)
		}
	}
	sort.Ints(cpus)
	if len(cpus) == 0 {
		cpus = []int{1}
		log.Println("Nessun file trovato, default a CPU1")
	}
	log.Printf("CPU disponibili finali: %v", cpus)
	return cpus, nil
}
