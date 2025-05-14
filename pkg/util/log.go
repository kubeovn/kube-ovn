package util

import (
	"log"
	"os"
)

func InitLogFilePerm(moduleName string, perm os.FileMode) {
	logPath := "/var/log/kube-ovn/" + moduleName + ".log"
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, perm)
		if err != nil {
			log.Fatalf("failed to create log file: %v", err)
		}
		f.Close()
	} else {
		if err := os.Chmod(logPath, perm); err != nil {
			log.Fatalf("failed to chmod log file: %v", err)
		}
	}
}
