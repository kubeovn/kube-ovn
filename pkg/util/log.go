package util

import (
	"log"
	"os"
)

func InitLogFilePerm(ModuleName string) {
	logPath := "/var/log/kube-ovn/" + ModuleName + ".log"
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0640)
		if err != nil {
			log.Fatalf("failed to create log file: %v", err)
		}
		f.Close()
	} else {
		if err := os.Chmod(logPath, 0640); err != nil {
			log.Fatalf("failed to chmod log file: %v", err)
		}
	}
}
