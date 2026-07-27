package main

import (
	"syscall"
	"unsafe"
)

// InvocatoreSyscallNativo esegue uno sdoppiamento a basso livello tramite il Kernel Linux.
func InvocatoreSyscallNativo(bin string, argv []string, envv []string) (int, error) {
	sysBin, _ := syscall.BytePtrFromString(bin)
	
	// Creiamo l'array di puntatori a byte per gli argomenti
	sysArgv := make([]*byte, len(argv)+1)
	for i, arg := range argv {
		sysArgv[i], _ = syscall.BytePtrFromString(arg)
	}
	sysArgv[len(argv)] = nil

	// Creiamo l'array di puntatori a byte per l'ambiente
	sysEnvv := make([]*byte, len(envv)+1)
	for i, env := range envv {
		sysEnvv[i], _ = syscall.BytePtrFromString(env)
	}
	sysEnvv[len(envv)] = nil

	// Invochiamo la syscall cruda SYS_FORK (Numero 2)
	pid, _, err := syscall.Syscall(syscall.SYS_FORK, 0, 0, 0)
	if err != 0 {
		return 0, err
	}

	if pid == 0 {
		// --- PROCESSO FIGLIO ---
		// CORREZIONE: Passiamo l'indirizzo del PRIMO ELEMENTO dell'array di puntatori,
		// che è l'esatto formato richiesto dalla syscall SYS_EXECVE (Numero 11) a 32-bit.
		_, _, _ = syscall.Syscall(syscall.SYS_EXECVE, 
			uintptr(unsafe.Pointer(sysBin)), 
			uintptr(unsafe.Pointer(&sysArgv[0])), 
			uintptr(unsafe.Pointer(&sysEnvv[0])),
		)
		// Se l'execve fallisce (es. se /bin/bash non risponde), esce con 127
		syscall.Exit(127)
	}

	// --- PROCESSO PADRE ---
	return int(pid), nil
}