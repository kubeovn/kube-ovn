package util

import (
	"encoding/json"
	"fmt"
	"strings"

	ps "github.com/bhendo/go-powershell"
	"github.com/bhendo/go-powershell/backend"
	"k8s.io/klog/v2"
)

const (
	WindowsAddressFamilyV4 = 2
	WindowsAddressFamilyV6 = 23
)

// NetAdapter represents a network adapter on windows
type NetAdapter struct {
	Name                       string
	ElementName                string
	MacAddress                 string
	InterfaceIndex             uint32
	InterfaceAdminStatus       uint32
	InterfaceOperationalStatus uint32
}

// NetIPInterface represents a net IP interface on windows
type NetIPInterface struct {
	InterfaceIndex uint32
	InterfaceAlias string
	AddressFamily  uint16
	NlMtu          uint32
	Forwarding     uint8
	Dhcp           uint8
}

// NetIPAddress represents a net IP address on windows
type NetIPAddress struct {
	InterfaceIndex uint32
	InterfaceAlias string
	AddressFamily  uint16
	IPAddress      string
	PrefixLength   uint8
}

// NetRoute represents a net route on windows
type NetRoute struct {
	InterfaceIndex    uint32
	InterfaceAlias    string
	AddressFamily     uint16
	DestinationPrefix string
	NextHop           string
}

func bool2PsParam(v bool) string {
	if v {
		return "Enabled"
	}
	return "Disabled"
}

func Powershell(cmd string) (string, error) {
	shell, err := ps.New(&backend.Local{})
	if err != nil {
		return "", err
	}
	defer shell.Exit()

	stdout, _, err := shell.Execute(cmd)
	if err != nil {
		return stdout, err
	}
	return stdout, nil
}

func GetNetAdapter(name string, ignoreError bool) (*NetAdapter, error) {
	output, err := Powershell(fmt.Sprintf(`Get-NetAdapter -Name "%s" | ConvertTo-Json`, name))
	if err != nil {
		if !ignoreError {
			err2 := fmt.Errorf("failed to get network adapter %s: %v", name, err)
			klog.Error(err2)
			return nil, err2
		}
		return nil, nil
	}

	adapter := &NetAdapter{}
	if err = json.Unmarshal([]byte(output), adapter); err != nil {
		err2 := fmt.Errorf("failed to parse information of network adapter %s: %v", name, err)
		klog.Error(err2)
		return nil, err2
	}

	adapter.MacAddress = strings.ReplaceAll(adapter.MacAddress, "-", ":")
	return adapter, nil
}

func EnableAdapter(adapter string) error {
	_, err := Powershell(fmt.Sprintf(`Enable-NetAdapter -Name "%s" -Confirm:$False`, adapter))
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to enable network adapter %s: %v", adapter, err)
	}
	return nil
}

func SetAdapterMac(adapter, mac string) error {
	_, err := Powershell(fmt.Sprintf(`Set-NetAdapter -Name "%s" -MacAddress %s -Confirm:$False`, adapter, mac))
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to set MAC address of network adapter %s: %v", adapter, err)
	}
	return nil
}

func GetNetIPInterface(ifIndex uint32) ([]NetIPInterface, error) {
	output, err := Powershell(fmt.Sprintf("Get-NetIPInterface -InterfaceIndex %d | ConvertTo-Json", ifIndex))
	if err != nil {
		err2 := fmt.Errorf("failed to get NetIPInterface with index %d: %v", ifIndex, err)
		klog.Error(err2)
		return nil, err2
	}

	result := make([]NetIPInterface, 0, 2)
	if err = json.Unmarshal([]byte(output), &result); err != nil {
		err2 := fmt.Errorf("failed to parse information of NetIPInterface: %v", err)
		klog.Error(err2)
		return nil, err2
	}

	return result, nil
}

func SetNetIPInterface(ifIndex uint32, addressFamily *uint16, mtu *uint32, dhcp, forwarding *bool) error {
	parameters := make([]string, 0)
	if addressFamily != nil {
		parameters = append(parameters, fmt.Sprintf("-AddressFamily %d", *addressFamily))
	}
	if mtu != nil {
		parameters = append(parameters, fmt.Sprintf("-NlMtuBytes %d", *mtu))
	}
	if dhcp != nil {
		parameters = append(parameters, fmt.Sprintf("-Dhcp %s", bool2PsParam(*dhcp)))
	}
	if forwarding != nil {
		parameters = append(parameters, fmt.Sprintf("-Forwarding %s", bool2PsParam(*forwarding)))
	}

	_, err := Powershell(fmt.Sprintf("Set-NetIPInterface -IncludeAllCompartments -InterfaceIndex %d %s -Confirm:$False", ifIndex, strings.Join(parameters, " ")))
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to set NetIPInterface with index %d: %v", ifIndex, err)
	}

	return nil
}

func GetNetIPAddress(ifIndex uint32) ([]NetIPAddress, error) {
	output, err := Powershell(fmt.Sprintf("Get-NetIPAddress -InterfaceIndex %d | ConvertTo-Json", ifIndex))
	if err != nil {
		err2 := fmt.Errorf("failed to get NetIPAddress with index %d: %v", ifIndex, err)
		klog.Error(err2)
		return nil, err2
	}

	if output[0] == '{' {
		output = fmt.Sprintf("[%s]", output)
	}

	result := make([]NetIPAddress, 0, 2)
	if err = json.Unmarshal([]byte(output), &result); err != nil {
		err2 := fmt.Errorf("failed to parse information of NetIPAddress: %v", err)
		klog.Error(err2)
		return nil, err2
	}

	return result, nil
}

func NewNetIPAddress(ifIndex uint32, ipAddr string) error {
	fields := strings.Split(ipAddr, "/")
	cmd := fmt.Sprintf("New-NetIPAddress -InterfaceIndex %d -IPAddress %s -PrefixLength %s -PolicyStore ActiveStore -Confirm:$False", ifIndex, fields[0], fields[1])
	_, err := Powershell(cmd)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to add IP address %s to interface with index %d: %v", ipAddr, ifIndex, err)
	}
	return nil
}

func RemoveNetIPAddress(ifIndex uint32, ipAddr string) error {
	fields := strings.Split(ipAddr, "/")
	cmd := fmt.Sprintf("Remove-NetIPAddress -InterfaceIndex %d -IPAddress %s -PrefixLength %s -Confirm:$False", ifIndex, fields[0], fields[1])
	_, err := Powershell(cmd)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to remove IP address %s from interface with index %d: %v", ipAddr, ifIndex, err)
	}
	return nil
}

func GetInterfaceByIP(ip string) (*NetIPInterface, error) {
	output, err := Powershell(fmt.Sprintf(`Get-NetIPAddress | Where-Object -Property IPAddress -eq -Value "%s" | ConvertTo-Json`, ip))
	if err != nil {
		err2 := fmt.Errorf("failed to get interface by IP %s: %v", ip, err)
		klog.Error(err2)
		return nil, err2
	}
	if len(output) == 0 {
		err = fmt.Errorf("interface with IP %s not found:")
		klog.Error(err)
		return nil, err
	}

	var ipAddr NetIPAddress
	if err = json.Unmarshal([]byte(output), &ipAddr); err != nil {
		err2 := fmt.Errorf("failed to parse information of NetIPAddress: %v", err)
		klog.Error(err2)
		return nil, err2
	}

	interfaces, err := GetNetIPInterface(ipAddr.InterfaceIndex)
	if err != nil {
		return nil, err
	}
	for _, iface := range interfaces {
		if iface.AddressFamily == ipAddr.AddressFamily {
			return &iface, nil
		}
	}

	return nil, fmt.Errorf("failed to get interface with address family %d", ipAddr.AddressFamily, err)
}

func GetNetRoute(ifIndex uint32) ([]NetRoute, error) {
	output, err := Powershell(fmt.Sprintf("Get-NetRoute -InterfaceIndex %d | ConvertTo-Json", ifIndex))
	if err != nil {
		err2 := fmt.Errorf("failed to get NetRoute with index %d: %v", ifIndex, err)
		klog.Error(err2)
		return nil, err2
	}

	result := make([]NetRoute, 0, 2)
	if err = json.Unmarshal([]byte(output), &result); err != nil {
		err2 := fmt.Errorf("failed to parse information of NetRoute: %v", err)
		klog.Error(err2)
		return nil, err2
	}

	return result, nil
}

func NewNetRoute(ifIndex uint32, destinationPrefix, nextHop string) error {
	cmd := fmt.Sprintf("New-NetRoute -InterfaceIndex %d -DestinationPrefix %s -NextHop %s -PolicyStore ActiveStore -Confirm:$False", ifIndex, destinationPrefix, nextHop)
	_, err := Powershell(cmd)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to add route %s nexthop %s to interface with index %d: %v", destinationPrefix, nextHop, ifIndex, err)
	}
	return nil
}

func RemoveNetRoute(ifIndex uint32, destinationPrefix string) error {
	cmd := fmt.Sprintf("Remove-NetRoute -InterfaceIndex %d -DestinationPrefix %s -Confirm:$False", ifIndex, destinationPrefix)
	_, err := Powershell(cmd)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to remove route %s from interface with index %d: %v", destinationPrefix, ifIndex, err)
	}
	return nil
}

func NewNetNat(name, addressPrefix string, mayExist bool) error {
	cmd := fmt.Sprintf(`New-NetNat -Name "%s" -InternalIPInterfaceAddressPrefix %s`, name, addressPrefix)
	if mayExist {
		cmd = fmt.Sprintf(`if (!(Get-NetNat | Where {$_.Name -eq "%s"})) {%s}`, name, cmd)
	}
	_, err := Powershell(cmd)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to add net nat %s with address prefix %s: %v", name, addressPrefix, err)
	}
	return nil
}
