package sriovnet

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	utilfs "github.com/Mellanox/sriovnet/pkg/utils/filesystem"
)

const (
	netdevPhysSwitchID = "phys_switch_id"
	netdevPhysPortName = "phys_port_name"
)

var physPortRe = regexp.MustCompile(`pf(\d+)vf(\d+)`)

func parsePortName(physPortName string) (pfRepIndex int, vfRepIndex int, err error) {

	pfRepIndex = -1
	vfRepIndex = -1

	// old kernel syntax of phys_port_name is vf index
	physPortName = strings.TrimSpace(physPortName)
	physPortNameInt, err := strconv.Atoi(physPortName)
	if err == nil {
		vfRepIndex = physPortNameInt
	} else {
		// new kernel syntax of phys_port_name pfXVfY
		matches := physPortRe.FindStringSubmatch(physPortName)
		if len(matches) != 3 {
			err = fmt.Errorf("Failed to parse physPortName %s", physPortName)
		} else {
			pfRepIndex, err = strconv.Atoi(matches[1])
			if err == nil {
				vfRepIndex, err = strconv.Atoi(matches[2])
			}
		}

	}
	return pfRepIndex, vfRepIndex, err
}

func isSwitchdev(netdevice string) bool {
	swIDFile := filepath.Join(NetSysDir, netdevice, netdevPhysSwitchID)
	physSwitchID, err := utilfs.Fs.ReadFile(swIDFile)
	if err != nil {
		return false
	}
	if physSwitchID != nil && string(physSwitchID) != "" {
		return true
	}
	return false
}

// GetUplinkRepresentor gets a VF PCI address (e.g '0000:03:00.4') and
// returns the uplink represntor netdev name for that VF.
func GetUplinkRepresentor(vfPciAddress string) (string, error) {
	devicePath := filepath.Join(PciSysDir, vfPciAddress, "physfn/net")
	devices, err := utilfs.Fs.ReadDir(devicePath)
	if err != nil {
		return "", fmt.Errorf("failed to lookup %s: %v", vfPciAddress, err)
	}
	for _, device := range devices {
		if isSwitchdev(device.Name()) {
			return device.Name(), nil
		}
	}
	return "", fmt.Errorf("uplink for %s not found", vfPciAddress)
}

func GetVfRepresentor(uplink string, vfIndex int) (string, error) {
	swIDFile := filepath.Join(NetSysDir, uplink, netdevPhysSwitchID)
	physSwitchID, err := utilfs.Fs.ReadFile(swIDFile)
	if err != nil || string(physSwitchID) == "" {
		return "", fmt.Errorf("cant get uplink %s switch id", uplink)
	}

	pfSubsystemPath := filepath.Join(NetSysDir, uplink, "subsystem")
	devices, err := utilfs.Fs.ReadDir(pfSubsystemPath)
	if err != nil {
		return "", err
	}
	for _, device := range devices {
		devicePath := filepath.Join(NetSysDir, device.Name())
		deviceSwIDFile := filepath.Join(devicePath, netdevPhysSwitchID)
		deviceSwID, err := utilfs.Fs.ReadFile(deviceSwIDFile)
		if err != nil || string(deviceSwID) != string(physSwitchID) {
			continue
		}
		devicePortNameFile := filepath.Join(devicePath, netdevPhysPortName)
		_, err = os.Stat(devicePortNameFile)
		if os.IsNotExist(err) {
			continue
		}
		physPortName, err := utilfs.Fs.ReadFile(devicePortNameFile)
		if err != nil {
			continue
		}
		physPortNameStr := string(physPortName)
		pfRepIndex, vfRepIndex, _ := parsePortName(physPortNameStr)
		if pfRepIndex != -1 {
			pfPCIAddress, err := getPCIFromDeviceName(uplink)
			if err != nil {
				continue
			}
			PCIFuncAddress, err := strconv.Atoi(string((pfPCIAddress[len(pfPCIAddress)-1])))
			if pfRepIndex != PCIFuncAddress || err != nil {
				continue
			}
		}
		// At this point we're confident we have a representor.
		if vfRepIndex == vfIndex {
			return device.Name(), nil
		}
	}
	return "", fmt.Errorf("failed to find VF representor for uplink %s", uplink)
}
