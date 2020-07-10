package sriovnet

import (
	"net"
	"path/filepath"
	"strconv"
)

const (
	ibSriovCfgDir               = "sriov"
	ibSriovNodeFile             = "node"
	ibSriovPortFile             = "port"
	ibSriovPortAdminFile        = "policy"
	ibSriovPortAdminStateFollow = "Follow"
)

func ibGetPortAdminState(pfNetdevName string, vfIndex int) (string, error) {
	path := filepath.Join(NetSysDir, pfNetdevName, pcidevPrefix, ibSriovCfgDir, strconv.Itoa(vfIndex), ibSriovPortAdminFile)
	adminStateFile := fileObject{
		Path: path,
	}

	state, err := adminStateFile.Read()
	if err != nil {
		return "", err
	}
	return state, nil
}

func ibSetPortAdminState(pfNetdevName string, vfIndex int, newState string) error {
	path := filepath.Join(NetSysDir, pfNetdevName, pcidevPrefix, ibSriovCfgDir, strconv.Itoa(vfIndex), ibSriovPortAdminFile)
	adminStateFile := fileObject{
		Path: path,
	}

	return adminStateFile.Write(newState)
}

func ibSetNodeGuid(pfNetdevName string, vfIndex int, guid net.HardwareAddr) error {
	path := filepath.Join(NetSysDir, pfNetdevName, pcidevPrefix, ibSriovCfgDir, strconv.Itoa(vfIndex), ibSriovNodeFile)
	nodeGuidFile := fileObject{
		Path: path,
	}
	kernelGuidFormat := guid.String()
	return nodeGuidFile.Write(kernelGuidFormat)
}

func ibSetPortGuid(pfNetdevName string, vfIndex int, guid net.HardwareAddr) error {
	path := filepath.Join(NetSysDir, pfNetdevName, pcidevPrefix, ibSriovCfgDir, strconv.Itoa(vfIndex), ibSriovPortFile)
	portGuidFile := fileObject{
		Path: path,
	}
	kernelGuidFormat := guid.String()
	return portGuidFile.Write(kernelGuidFormat)
}
