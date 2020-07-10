package sriovnet

import (
	"fmt"
	"github.com/google/uuid"
	"github.com/vishvananda/netlink"
	"log"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	utilfs "github.com/Mellanox/sriovnet/pkg/utils/filesystem"
)

var virtFnRe = regexp.MustCompile(`virtfn(\d+)`)

type VfObj struct {
	Index      int
	PciAddress string
	Bound      bool
	Allocated  bool
}

type PfNetdevHandle struct {
	PfNetdevName string
	pfLinkHandle netlink.Link

	List []*VfObj
}

func SetPFLinkUp(pfNetdevName string) error {
	handle, err := netlink.LinkByName(pfNetdevName)
	if err != nil {
		return err
	}

	return netlink.LinkSetUp(handle)
}

func IsSriovSupported(netdevName string) bool {

	maxvfs, err := getMaxVfCount(netdevName)
	if maxvfs == 0 || err != nil {
		return false
	} else {
		return true
	}
}

func IsSriovEnabled(netdevName string) bool {

	curvfs, err := getCurrentVfCount(netdevName)
	if curvfs == 0 || err != nil {
		return false
	} else {
		return true
	}
}

func EnableSriov(pfNetdevName string) error {
	var maxVfCount int
	var err error

	devDirName := netDevDeviceDir(pfNetdevName)

	devExist := dirExists(devDirName)
	if !devExist {
		return fmt.Errorf("device %s not found", pfNetdevName)
	}

	maxVfCount, err = getMaxVfCount(pfNetdevName)
	if err != nil {
		log.Println("Fail to read max vf count of PF", pfNetdevName)
		return err
	}

	if maxVfCount == 0 {
		return fmt.Errorf("sriov unsupported for device: %s", pfNetdevName)
	}

	curVfCount, err2 := getCurrentVfCount(pfNetdevName)
	if err2 != nil {
		log.Println("Fail to read current vf count of PF", pfNetdevName)
		return err
	}
	if curVfCount == 0 {
		return setMaxVfCount(pfNetdevName, maxVfCount)
	}
	return nil
}

func DisableSriov(pfNetdevName string) error {
	devDirName := netDevDeviceDir(pfNetdevName)

	devExist := dirExists(devDirName)
	if !devExist {
		return fmt.Errorf("device %s not found", pfNetdevName)
	}

	return setMaxVfCount(pfNetdevName, 0)
}

func GetPfNetdevHandle(pfNetdevName string) (*PfNetdevHandle, error) {

	pfLinkHandle, err := netlink.LinkByName(pfNetdevName)
	if err != nil {
		return nil, err
	}

	handle := PfNetdevHandle{
		PfNetdevName: pfNetdevName,
		pfLinkHandle: pfLinkHandle,
	}

	list, err := GetVfPciDevList(pfNetdevName)
	if err != nil {
		return nil, err
	}

	for _, vfDir := range list {
		vfIndexStr := strings.TrimPrefix(vfDir, netDevVfDevicePrefix)
		vfIndex, _ := strconv.Atoi(vfIndexStr)
		vfNetdevName := vfNetdevNameFromParent(pfNetdevName, vfIndex)
		pciAddress, err := vfPCIDevNameFromVfIndex(pfNetdevName, vfIndex)
		if err != nil {
			log.Printf("Failed to read PCI Address for VF %v from PF %v: %v\n",
				vfNetdevName, pfNetdevName, err)
			continue
		}
		vfObj := VfObj{
			Index:      vfIndex,
			PciAddress: pciAddress,
		}
		if vfNetdevName != "" {
			vfObj.Bound = true
		} else {
			vfObj.Bound = false
		}
		vfObj.Allocated = false
		handle.List = append(handle.List, &vfObj)
	}
	return &handle, nil
}

func UnbindVf(handle *PfNetdevHandle, vf *VfObj) error {
	cmdFile := filepath.Join(NetSysDir, handle.PfNetdevName, netdevDriverDir, netdevUnbindFile)
	cmdFileObj := fileObject{
		Path: cmdFile,
	}
	err := cmdFileObj.Write(vf.PciAddress)
	if err != nil {
		vf.Bound = false
	}
	return err
}

func BindVf(handle *PfNetdevHandle, vf *VfObj) error {
	cmdFile := filepath.Join(NetSysDir, handle.PfNetdevName, netdevDriverDir, netdevBindFile)
	cmdFileObj := fileObject{
		Path: cmdFile,
	}
	err := cmdFileObj.Write(vf.PciAddress)
	if err != nil {
		vf.Bound = true
	}
	return err
}

func GetVfDefaultMacAddr(vfNetdevName string) (string, error) {

	ethHandle, err1 := netlink.LinkByName(vfNetdevName)
	if err1 != nil {
		return "", err1
	}

	ethAttr := ethHandle.Attrs()
	return ethAttr.HardwareAddr.String(), nil
}

func SetVfDefaultMacAddress(handle *PfNetdevHandle, vf *VfObj) error {

	netdevName := vfNetdevNameFromParent(handle.PfNetdevName, vf.Index)
	ethHandle, err1 := netlink.LinkByName(netdevName)
	if err1 != nil {
		return err1
	}
	ethAttr := ethHandle.Attrs()
	return netlink.LinkSetVfHardwareAddr(handle.pfLinkHandle, vf.Index, ethAttr.HardwareAddr)
}

func SetVfVlan(handle *PfNetdevHandle, vf *VfObj, vlan int) error {
	return netlink.LinkSetVfVlan(handle.pfLinkHandle, vf.Index, vlan)
}

func setVfNodeGuid(handle *PfNetdevHandle, vf *VfObj, guid []byte) error {
	var err error

	nodeGuidHwAddr := net.HardwareAddr(guid)

	err = ibSetNodeGuid(handle.PfNetdevName, vf.Index, nodeGuidHwAddr)
	if err == nil {
		return nil
	}
	err = netlink.LinkSetVfNodeGUID(handle.pfLinkHandle, vf.Index, guid)
	return err
}

func setVfPortGuid(handle *PfNetdevHandle, vf *VfObj, guid []byte) error {
	var err error

	portGuidHwAddr := net.HardwareAddr(guid)

	err = ibSetPortGuid(handle.PfNetdevName, vf.Index, portGuidHwAddr)
	if err == nil {
		return nil
	}
	err = netlink.LinkSetVfPortGUID(handle.pfLinkHandle, vf.Index, guid)
	return err
}

func SetVfDefaultGUID(handle *PfNetdevHandle, vf *VfObj) error {

	uuid, err := uuid.NewRandom()
	if err != nil {
		return err
	}
	guid := uuid[0:8]
	guid[7] = byte(vf.Index)

	err = setVfNodeGuid(handle, vf, guid)
	if err != nil {
		return err
	}

	err = setVfPortGuid(handle, vf, guid)
	return err
}

func SetVfPrivileged(handle *PfNetdevHandle, vf *VfObj, privileged bool) error {

	var spoofChk bool
	var trusted bool

	ethAttr := handle.pfLinkHandle.Attrs()
	if ethAttr.EncapType != "ether" {
		return nil
	}
	//Only ether type is supported
	if privileged {
		spoofChk = false
		trusted = true
	} else {
		spoofChk = true
		trusted = false
	}

	/* do not check for error status as older kernels doesn't
	 * have support for it.
	 * golangci-lint complains on missing error check. ignore it
	 * with nolint comment until we update the code to ignore ENOTSUP error
	 */
	netlink.LinkSetVfTrust(handle.pfLinkHandle, vf.Index, trusted)     //nolint
	netlink.LinkSetVfSpoofchk(handle.pfLinkHandle, vf.Index, spoofChk) //nolint
	return nil
}

func setDefaultHwAddr(handle *PfNetdevHandle, vf *VfObj) error {
	var err error

	ethAttr := handle.pfLinkHandle.Attrs()
	if ethAttr.EncapType == "ether" {
		err = SetVfDefaultMacAddress(handle, vf)
	} else if ethAttr.EncapType == "infiniband" {
		err = SetVfDefaultGUID(handle, vf)
	}
	return err
}

func setPortAdminState(handle *PfNetdevHandle, vf *VfObj) error {
	ethAttr := handle.pfLinkHandle.Attrs()
	if ethAttr.EncapType == "infiniband" {
		state, err2 := ibGetPortAdminState(handle.PfNetdevName, vf.Index)
		// Ignore the error where this file is not available
		if err2 != nil {
			return nil
		}
		log.Printf("Admin state = %v", state)
		err2 = ibSetPortAdminState(handle.PfNetdevName, vf.Index, ibSriovPortAdminStateFollow)
		if err2 != nil {
			//If file exist, we must be able to write
			log.Printf("Admin state setting error = %v", err2)
			return err2
		}
	}
	return nil
}

func ConfigVfs(handle *PfNetdevHandle, privileged bool) error {
	var err error

	for _, vf := range handle.List {
		log.Printf("vf = %v\n", vf)
		err = setPortAdminState(handle, vf)
		if err != nil {
			break
		}
		// skip VFs in another namespace
		netdevName := vfNetdevNameFromParent(handle.PfNetdevName, vf.Index)
		if _, err = netlink.LinkByName(netdevName); err != nil {
			continue
		}
		err = setDefaultHwAddr(handle, vf)
		if err != nil {
			break
		}
		_ = SetVfPrivileged(handle, vf, privileged)
	}
	if err != nil {
		return err
	}
	for _, vf := range handle.List {
		if vf.Bound {
			err = UnbindVf(handle, vf)
			if err != nil {
				log.Printf("Fail to unbind err=%v\n", err)
				break
			}
			err = BindVf(handle, vf)
			if err != nil {
				log.Printf("Fail to bind err=%v\n", err)
				break
			}
			log.Printf("vf = %v unbind/bind completed", vf)
		}
	}
	return nil
}

func AllocateVf(handle *PfNetdevHandle) (*VfObj, error) {
	for _, vf := range handle.List {
		if vf.Allocated {
			continue
		}
		vf.Allocated = true
		log.Printf("Allocated vf = %v\n", *vf)
		return vf, nil
	}
	return nil, fmt.Errorf("All Vfs for %v are allocated.", handle.PfNetdevName)
}

func AllocateVfByMacAddress(handle *PfNetdevHandle, vfMacAddress string) (*VfObj, error) {
	for _, vf := range handle.List {
		if vf.Allocated {
			continue
		}

		netdevName := vfNetdevNameFromParent(handle.PfNetdevName, vf.Index)
		macAddr, _ := GetVfDefaultMacAddr(netdevName)
		if macAddr != vfMacAddress {
			continue
		}
		vf.Allocated = true
		log.Printf("Allocated vf by mac = %v\n", *vf)
		return vf, nil
	}
	return nil, fmt.Errorf("All Vfs for %v are allocated for mac address %v.",
		handle.PfNetdevName, vfMacAddress)
}

func FreeVf(handle *PfNetdevHandle, vf *VfObj) {
	vf.Allocated = false
	log.Printf("Free vf = %v\n", *vf)
}

func FreeVfByNetdevName(handle *PfNetdevHandle, vfIndex int) error {
	vfNetdevName := fmt.Sprintf("%s%v", netDevVfDevicePrefix, vfIndex)
	for _, vf := range handle.List {
		netdevName := vfNetdevNameFromParent(handle.PfNetdevName, vf.Index)
		if vf.Allocated && netdevName == vfNetdevName {
			vf.Allocated = true
			return nil
		}
	}
	return fmt.Errorf("vf netdev %v not found", vfNetdevName)
}

func GetVfNetdevName(handle *PfNetdevHandle, vf *VfObj) string {
	return vfNetdevNameFromParent(handle.PfNetdevName, vf.Index)
}

// GetVfIndexByPciAddress gets a VF PCI address (e.g '0000:03:00.4') and
// returns the correlate VF index.
func GetVfIndexByPciAddress(vfPciAddress string) (int, error) {
	vfPath := filepath.Join(PciSysDir, vfPciAddress, "physfn/virtfn*")
	matches, err := filepath.Glob(vfPath)
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

// GetNetDevicesFromPci gets a PCI address (e.g '0000:03:00.1') and
// returns the correlate list of netdevices
func GetNetDevicesFromPci(pciAddress string) ([]string, error) {
	var netDevices []string
	pciDir := filepath.Join(PciSysDir, pciAddress, "net")
	_, err := utilfs.Fs.Stat(pciDir)
	if err != nil {
		return netDevices, fmt.Errorf("cannot get a network device with pci address %v %v", pciAddress, err)
	}

	netDevicesFiles, err := utilfs.Fs.ReadDir(pciDir)
	if err != nil {
		return netDevices, fmt.Errorf("failed to get network device name in %v %v", pciDir, err)
	}
	for _, netDeviceFile := range netDevicesFiles {
		netDevices = append(netDevices, strings.TrimSpace(netDeviceFile.Name()))
	}
	return netDevices, nil
}
