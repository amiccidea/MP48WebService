package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

func rebootHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Esegui il reboot in una goroutine per non bloccare la risposta
	go func() {
		var err error
		if runtime.GOOS == "linux" {
			// Comando reboot (richiede privilegi di root)
			err = exec.Command("reboot").Run()
			if err != nil {
				log.Printf("Errore durante il reboot: %v", err)
			} else {
				log.Println("Reboot avviato dal web interface")
			}
		} else {
			log.Println("Reboot non supportato su questo sistema operativo")
		}
	}()

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Reboot avviato"))
}

// RebootRemoteDevice invia un comando di reboot a un dispositivo all'indirizzo IP specificato.
// Restituisce nil se il comando ha successo, altrimenti un errore.
func RebootRemoteDeviceWithConfig(ip string, userPass string) error {
	// userPass formato "root:password" (default "root:itacomanager")
	address := fmt.Sprintf("%s:3009", ip)
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	commands := []string{userPass, "use", "reboot", "quit"}
	for _, cmd := range commands {
		if _, err := fmt.Fprintf(conn, "%s\n", cmd); err != nil {
			return err
		}
		// Legge risposta (solo per controllare FAILURE)
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		buf := make([]byte, 256)
		n, _ := conn.Read(buf)
		if n > 0 && strings.Contains(string(buf[:n]), "FAILURE") {
			return fmt.Errorf("FAILURE per comando '%s'", cmd)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}
