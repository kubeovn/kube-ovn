package util

import (
	"log"
	"os"
)

func InitLogFile(ModuleName string) {
	logPath := "/var/log/kube-ovn/" + ModuleName + ".log"
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0640)
	if err != nil {
		log.Fatalf("failed to create log file: %v", err)
	}
	f.Close()
}
