package ipsec

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/kubeovn/kube-ovn/pkg/fileutil"
)

type generation struct {
	ID        string `json:"id"`
	NodeName  string `json:"nodeName,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	NodeUID   string `json:"nodeUID"`
	Chassis   string `json:"chassis"`
}

type store struct {
	dir string
}

func digest(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func (s store) lock() (*os.File, error) {
	if err := secureDirectory(s.dir); err != nil {
		return nil, err
	}
	if err := os.Chmod(s.dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(s.dir, "owner.lock"), os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, errors.Join(fmt.Errorf("another IPsec owner holds the node lock: %w", err), f.Close())
	}
	return f, nil
}

func (s store) load(name string) (*generation, error) {
	data, err := readRegularFile(filepath.Join(s.dir, name+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var g generation
	if err := json.Unmarshal(data, &g); err != nil {
		return nil, err
	}
	if len(g.ID) != 64 {
		return nil, errors.New("invalid IPsec generation")
	}
	if _, err := hex.DecodeString(g.ID); err != nil {
		return nil, err
	}
	return &g, nil
}

func (s store) save(name string, g *generation) error {
	data, err := json.Marshal(g)
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(filepath.Join(s.dir, name+".json"), data, 0o600)
}

func (s store) path(g *generation, name string) string {
	return filepath.Join(s.dir, "generations", g.ID, name+".pem")
}

func (s store) write(g *generation, name string, data []byte) error {
	path := s.path(g, name)
	if err := s.generationDirectory(g); err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(path, data, 0o600)
}

func (s store) read(g *generation, name string) ([]byte, error) {
	if err := s.generationDirectory(g); err != nil {
		return nil, err
	}
	return readRegularFile(s.path(g, name))
}

func secureDirectory(path string) error {
	if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("IPsec storage directory must not be a symlink")
	}
	return os.Chmod(path, 0o700)
}

func (s store) generationDirectory(g *generation) error {
	for _, path := range []string{s.dir, filepath.Join(s.dir, "generations"), filepath.Dir(s.path(g, "private-key"))} {
		if err := secureDirectory(path); err != nil {
			return err
		}
	}
	return nil
}

func readRegularFile(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err == nil && (!info.Mode().IsRegular() || info.Size() > 1<<20) {
		err = errors.New("IPsec storage entry must be a regular file smaller than 1 MiB")
	}
	var data []byte
	if err == nil {
		data, err = io.ReadAll(io.LimitReader(f, (1<<20)+1))
	}
	return data, errors.Join(err, f.Close())
}

// importLegacy adopts only the referenced, validated legacy pair in our key
// directory. It never deletes legacy files: an interrupted upgrade can still
// resume, and rollback policy is decided separately from identity migration.
func (s store) importLegacy(nodeUID, chassis string, trust []byte, paths map[string]string) error {
	for _, name := range []string{"current", "pending"} {
		g, err := s.load(name)
		if err != nil || g != nil {
			return err
		}
	}
	keyPath, certPath := paths["private_key"], paths["certificate"]
	if keyPath == "" && certPath == "" {
		return nil
	}
	for path, prefix := range map[string]string{keyPath: "ipsec-privkey-", certPath: "ipsec-cert-"} {
		if filepath.Dir(path) != filepath.Clean(s.dir) || !strings.HasPrefix(filepath.Base(path), prefix) || !strings.HasSuffix(path, ".pem") {
			return errors.New("cannot adopt IPsec files outside the legacy key layout")
		}
	}
	key, err := readRegularFile(keyPath)
	if err != nil {
		return err
	}
	cert, err := readRegularFile(certPath)
	if err != nil {
		return err
	}
	if _, err := validateIdentity(cert, key, trust, chassis, time.Now()); err != nil {
		// An expired legacy identity must be replaced, rather than permanently
		// preventing fresh signing. Unreadable paths are handled above.
		return nil
	}
	g := &generation{ID: digest(key), NodeUID: nodeUID, Chassis: chassis}
	if err := s.write(g, "private-key", key); err != nil {
		return err
	}
	if err := s.write(g, "certificate", cert); err != nil {
		return err
	}
	if err := s.recordGeneration(g); err != nil {
		return err
	}
	return s.save("pending", g)
}

func (s store) sameIdentity(a, b *generation) bool {
	if a.NodeUID != b.NodeUID || a.Chassis != b.Chassis {
		return false
	}
	for _, name := range []string{"private-key", "certificate"} {
		left, leftErr := s.read(a, name)
		right, rightErr := s.read(b, name)
		if leftErr != nil || rightErr != nil || !bytes.Equal(left, right) {
			return false
		}
	}
	return true
}

// prepareGeneration gives trust updates their own immutable paths. OVSDB can
// switch all three paths together without changing files used by the old IKE
// configuration during a failed or interrupted activation.
func (s store) prepareGeneration(source *generation, trust []byte) (*generation, error) {
	key, err := s.read(source, "private-key")
	if err != nil {
		return nil, err
	}
	cert, err := s.read(source, "certificate")
	if err != nil {
		return nil, err
	}
	data := append(append(append([]byte{}, key...), cert...), trust...)
	g := &generation{ID: digest(data), NodeName: source.NodeName, Namespace: source.Namespace, NodeUID: source.NodeUID, Chassis: source.Chassis}
	for name, value := range map[string][]byte{"private-key": key, "certificate": cert, "ca-bundle": trust} {
		path := s.path(g, name)
		if err := s.generationDirectory(g); err != nil {
			return nil, err
		}
		if existing, err := readRegularFile(path); err == nil {
			if !bytes.Equal(existing, value) {
				return nil, errors.New("IPsec generation content changed")
			}
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if err := s.write(g, name, value); err != nil {
			return nil, err
		}
	}
	if err := s.recordGeneration(g); err != nil {
		return nil, err
	}
	return g, nil
}

// pending persists the private key before submitting a request. Retries and
// container restarts therefore cannot consume a certificate for a different key.
func (s store) pending(nodeUID, chassis string) (*generation, []byte, error) {
	g, err := s.load("pending")
	if err != nil {
		return nil, nil, err
	}
	if g != nil && g.NodeUID == nodeUID && g.Chassis == chassis {
		key, err := s.read(g, "private-key")
		return g, key, err
	}
	key, err := newPrivateKey()
	if err != nil {
		return nil, nil, err
	}
	g = &generation{ID: digest(key), NodeUID: nodeUID, Chassis: chassis}
	if err := s.write(g, "private-key", key); err != nil {
		return nil, nil, err
	}
	if err := s.recordGeneration(g); err != nil {
		return nil, nil, err
	}
	if err := s.save("pending", g); err != nil {
		return nil, nil, err
	}
	return g, key, nil
}
