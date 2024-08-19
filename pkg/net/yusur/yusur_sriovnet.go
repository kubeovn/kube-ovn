package yusur

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	HwAddr        = "hw"
	YusurSmartNic = "smart-nic"
	PlatName      = "plat_name"
	PciSysDir     = "/sys/bus/pci/devices"
)

var virtFnRe = regexp.MustCompile(`virtfn(\d+)`)

// IsYusurSmartNic check is Yusur smart Nic
func IsYusurSmartNic(pciAddress string) bool {
	platFile := filepath.Join(PciSysDir, pciAddress, HwAddr, PlatName)

	absPath, err := filepath.Abs(platFile)
	if err != nil || !strings.HasPrefix(absPath, PciSysDir) {
		return false
	}

	platName, err := os.ReadFile(absPath)
	if err != nil {
		return false
	}

	yusurSmartNic := strings.TrimSpace(string(platName))
	return strings.HasSuffix(yusurSmartNic, YusurSmartNic)
}

// GetYusurNicPfPciFromVfPci retrieves the PF PCI address
func GetYusurNicPfPciFromVfPci(vfPciAddress string) (string, error) {
	pfPath := filepath.Join(PciSysDir, vfPciAddress, "physfn")
	absPath, err := filepath.Abs(pfPath)
	if err != nil || !strings.HasPrefix(absPath, PciSysDir) {
		return "", errors.New("pfPath is not ")
	}

	pciDevDir, err := os.Readlink(absPath)
	if err != nil {
		return "", fmt.Errorf("failed to read physfn link, provided address may not be a VF. %w", err)
	}

	pf := path.Base(pciDevDir)
	if pf == "" {
		return pf, errors.New("could not find PF PCI Address")
	}
	return pf, err
}

// GetYusurNicPfIndexByPciAddress gets a VF PCI address and
// returns the correlate PF index.
func GetYusurNicPfIndexByPciAddress(pfPci string) (int, error) {
	pfIndex, err := strconv.Atoi(string(pfPci[len(pfPci)-1]))
	if err != nil {
		return -1, fmt.Errorf("failed to get pfPci of device %s %w", pfPci, err)
	}

	return pfIndex, nil
}

// GetYusurNicVfIndexByPciAddress gets a VF PCI address and
// returns the correlate VF index.
func GetYusurNicVfIndexByPciAddress(vfPciAddress string) (int, error) {
	vfPath := filepath.Join(PciSysDir, vfPciAddress, "physfn", "virtfn*")
	absPath, err := filepath.Abs(vfPath)
	if err != nil || !strings.HasPrefix(absPath, PciSysDir) {
		return -1, errors.New("pfPath is not ")
	}

	matches, err := filepath.Glob(absPath)
	if err != nil {
		return -1, err
	}
	for _, match := range matches {
		tmp, err := os.Readlink(match)
		if err != nil {
			continue
		}
		if strings.Contains(tmp, vfPciAddress) {
			result := virtFnRe.FindStringSubmatch(match)
			vfIndex, err := strconv.Atoi(result[1])
			if err != nil {
				continue
			}
			return vfIndex, nil
		}
	}
	return -1, fmt.Errorf("vf index for %s not found", vfPciAddress)
}

// GetYusurNicVfRepresentor return representor name
func GetYusurNicVfRepresentor(pfIndex, vfIndex int) string {
	vfr := fmt.Sprintf("pf%dvf%drep", pfIndex, vfIndex)
	return vfr
}
