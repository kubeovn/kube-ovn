package frr

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	desiredFileName = "frr.conf.desired"
	applyFileName   = ".kube-ovn-frr-apply"
	appliedFileName = ".kube-ovn-frr-applied"
	resultFileName  = ".kube-ovn-frr-result"
)

type Applier struct {
	frrDir string
}

func NewApplier(frrDir string) *Applier {
	return &Applier{frrDir: frrDir}
}

func serial(config string) string {
	sum := sha256.Sum256([]byte(config))
	return hex.EncodeToString(sum[:8])
}

func (a *Applier) writeAtomic(name, content string) error {
	tmp := filepath.Join(a.frrDir, name+".tmp")
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(a.frrDir, name))
}

func (a *Applier) Apply(config string) (string, error) {
	s := serial(config)
	current, err := os.ReadFile(filepath.Join(a.frrDir, applyFileName))
	if err == nil && strings.TrimSpace(string(current)) == s {
		return s, nil
	}
	if err := a.writeAtomic(desiredFileName, config); err != nil {
		return "", fmt.Errorf("failed to write desired config: %w", err)
	}
	if err := a.writeAtomic(applyFileName, s+"\n"); err != nil {
		return "", fmt.Errorf("failed to write apply serial: %w", err)
	}
	return s, nil
}

type ApplyStatus struct {
	AppliedSerial string
	ResultState   string
	ResultSerial  string
	Detail        string
}

func (a *Applier) Status() ApplyStatus {
	var st ApplyStatus
	if data, err := os.ReadFile(filepath.Join(a.frrDir, appliedFileName)); err == nil {
		st.AppliedSerial = strings.TrimSpace(string(data))
	}
	data, err := os.ReadFile(filepath.Join(a.frrDir, resultFileName))
	if err != nil {
		return st
	}
	st.Detail = strings.TrimSpace(string(data))
	line, _, _ := strings.Cut(st.Detail, "\n")
	fields := strings.Fields(line)
	if len(fields) > 0 {
		st.ResultState = fields[0]
	}
	if len(fields) > 1 {
		st.ResultSerial = fields[1]
	}
	return st
}
