package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"syscall"
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

	// 1. Ottieni valori live (da script che genera file)
	pointsData, err := getAnalogPointsData(ioaStart, ioaEnd)
	if err != nil {
		log.Printf("⚠️ Errore getAnalogPointsData: %v", err)
		// Restituiamo lista vuota per evitare crash
		return []AnalogInput{}, nil
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

// getAnalogPointsData esegue lo script wrapper e legge il file punti analogici
func getAnalogPointsData(ioaStart, ioaEnd int) (map[int]analogPointData, error) {
	result := make(map[int]analogPointData)

	// Mock per Windows
	if runtime.GOOS == "windows" {
		for ioa := ioaStart; ioa <= ioaEnd; ioa++ {
			result[ioa] = analogPointData{
				Value:     float64(ioa%100) * 1.23,
				Timestamp: time.Now().Format("2006-01-02 15:04:05"),
			}
		}
		return result, nil
	}

	// Determina lo script da eseguire e il file di output
	scriptPath := config.AnalogInputsCmd
	filePath := config.AnalogPointsFile
	if filePath == "" {
		filePath = "analog_points.txt"
	}

	// Se lo script è configurato, eseguilo per generare il file
	if scriptPath != "" {
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
	} else {
		log.Printf("ℹ️ analog_inputs_cmd non configurato, provo a leggere file esistente")
	}

	// Ora leggi il file (dovrebbe essere stato creato dallo script, oppure esiste già)
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		log.Printf("❌ Errore lettura %s: %v", filePath, err)
		return result, nil
	}
	output := string(data)
	log.Printf("✅ File analog_points letto (%d byte)", len(output))

	// Parser dell'output per estrarre i valori
	return parseAnalogMatrix(output, ioaStart, ioaEnd)
}

// parseAnalogMatrix parsa l'output di rsl_smm -m (o file con stesso formato) e restituisce mappa IOA->valore
func parseAnalogMatrix(output string, ioaStart, ioaEnd int) (map[int]analogPointData, error) {
	result := make(map[int]analogPointData)

	// Mappa per memorizzare i valori per scheda
	cardValues := make(map[int][]float64) // key: numero scheda (1,2,3,4), value: array di 8 float

	scanner := bufio.NewScanner(strings.NewReader(output))
	inMatrix := false

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Cerca l'inizio della matrice
		if strings.Contains(trimmed, "[ MATRIX MEASURES ]") || strings.Contains(trimmed, "MATRIX MEASURES") {
			inMatrix = true
			continue
		}

		if !inMatrix {
			continue
		}

		// Salta righe di separazione
		if strings.HasPrefix(trimmed, "-----") || strings.HasPrefix(trimmed, "------") {
			continue
		}

		// Se la riga contiene "MP card", estrai i valori
		if strings.Contains(trimmed, "MP card") {
			fields := strings.Fields(trimmed)

			// Trova il numero della scheda
			var cardNum int
			for i, token := range fields {
				if token == "MP" && i+2 < len(fields) {
					if strings.Contains(fields[i+1], "card") {
						// Es: "MP card 1"
						if i+2 < len(fields) {
							if val, err := strconv.Atoi(fields[i+2]); err == nil {
								cardNum = val
								break
							}
						}
					}
				}
			}

			if cardNum == 0 {
				log.Printf("⚠️ Numero scheda non trovato nella riga: %s", trimmed)
				continue
			}

			// Estrai i primi 8 valori numerici dalla riga
			var values []float64
			for _, token := range fields {
				if val, err := strconv.ParseFloat(token, 64); err == nil {
					values = append(values, val)
					if len(values) >= 8 {
						break
					}
				}
			}

			if len(values) < 8 {
				log.Printf("⚠️ Meno di 8 valori trovati per scheda %d: %v", cardNum, values)
				continue
			}

			// Salva nella mappa
			cardValues[cardNum] = values
			log.Printf("✅ Scheda %d: valori = %v", cardNum, values)
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("⚠️ Errore scanner: %v", err)
	}

	// Se non abbiamo trovato schede, restituisci risultati vuoti
	if len(cardValues) == 0 {
		log.Printf("⚠️ Nessuna scheda MP trovata nell'output")
		return result, nil
	}

	// Mappa i valori agli IOA
	// Assumiamo che per la CPU1 (IOA 150-159), la scheda 1 sia la CPU1, scheda 2 -> CPU2, ecc.
	for cardNum, values := range cardValues {
		var baseIOA int
		switch cardNum {
		case 1:
			baseIOA = 150
		case 2:
			baseIOA = 250
		case 3:
			baseIOA = 350
		case 4:
			baseIOA = 450
		default:
			log.Printf("⚠️ Numero scheda non valido: %d", cardNum)
			continue
		}

		for i, val := range values {
			ioa := baseIOA + i
			if ioa >= ioaStart && ioa <= ioaEnd {
				result[ioa] = analogPointData{
					Value:     val,
					OV:        0,
					BL:        0,
					SB:        0,
					NT:        0,
					IV:        0,
					Timestamp: time.Now().Format("2006-01-02 15:04:05"),
				}
			}
		}
	}

	log.Printf("✅ Mappati %d punti analogici per IOA da %d a %d", len(result), ioaStart, ioaEnd)
	return result, nil
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