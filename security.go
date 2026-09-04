package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ValidatePath verifica che il percorso richiesto sia all'interno della directory base consentita.
// Restituisce il percorso assoluto pulito o un errore.
func ValidatePath(baseDir, inputPath string) (string, error) {
	if baseDir == "" {
		return "", errors.New("directory base non configurata")
	}
	if inputPath == "" {
		return "", errors.New("percorso richiesto vuoto")
	}

	// Pulizia dei percorsi
	cleanBase := filepath.Clean(baseDir)
	cleanInput := filepath.Clean(inputPath)

	// Se l'input è un percorso assoluto, rifiutiamo
	if filepath.IsAbs(cleanInput) {
		return "", errors.New("percorso assoluto non consentito")
	}

	// Costruisci il percorso completo
	fullPath := filepath.Join(cleanBase, cleanInput)

	// Normalizza il percorso completo
	fullPath = filepath.Clean(fullPath)

	// Verifica che il percorso completo inizi con la directory base pulita
	if !strings.HasPrefix(fullPath, cleanBase+string(filepath.Separator)) && fullPath != cleanBase {
		return "", fmt.Errorf("percorso non consentito: %s", fullPath)
	}

	return fullPath, nil
}