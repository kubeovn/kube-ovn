package ipam

import (
	"context"
	"errors"
	"maps"
	"net"
	"slices"
)

var (
	ErrTransactionClosed    = errors.New("IPAM transaction is closed")
	ErrTransactionNilAction = errors.New("IPAM transaction action is nil")
)

// Transaction stages IPAM allocation and release changes on private subnet
// copies. WithTransaction holds the IPAM lock for the complete action and
// atomically publishes the copies only when the action succeeds.
//
// The action may perform external API or OVN I/O, but all IPAM access must use
// the transaction methods. Calling IPAM methods directly from the action would
// deadlock because the transaction owns the IPAM lock.
type Transaction struct {
	ipam    *IPAM
	subnets map[string]*Subnet
	closed  bool
}

// WithTransaction runs action against isolated IPAM subnet copies. Returning
// an error discards every staged allocation or release. A successful action
// atomically replaces the touched subnet entries in IPAM.
func (ipam *IPAM) WithTransaction(action func(*Transaction) error) error {
	if action == nil {
		return ErrTransactionNilAction
	}

	ipam.mutex.Lock()
	defer ipam.mutex.Unlock()
	return ipam.withTransactionLocked(action)
}

func (ipam *IPAM) withTransactionLocked(action func(*Transaction) error) error {
	tx := &Transaction{ipam: ipam, subnets: make(map[string]*Subnet)}
	defer func() { tx.closed = true }()
	if err := action(tx); err != nil {
		return err
	}
	tx.commit()
	return nil
}

// WithTransactionContext runs an atomic transaction with cancellation checks
// before and after the action. Cancellation cannot interrupt a transaction
// while it owns the IPAM mutex; when it is observed after the action, the
// transaction has already committed or rolled back and the returned error
// reports that outcome as uncertain to the caller.
func (ipam *IPAM) WithTransactionContext(ctx context.Context, action func(*Transaction) error) error {
	if ctx == nil {
		return errors.New("ipam transaction context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if action == nil {
		return ErrTransactionNilAction
	}

	ipam.mutex.Lock()
	defer ipam.mutex.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return errors.Join(ipam.withTransactionLocked(action), ctx.Err())
}

// GetRandomAddress stages a random allocation on the transaction's subnet.
func (tx *Transaction) GetRandomAddress(podName, nicName string, mac *string, subnetName, poolName string, skippedAddrs []string, checkConflict bool) (string, string, string, error) {
	return tx.GetRandomAddressWithFamily(podName, nicName, mac, subnetName, poolName, "", skippedAddrs, checkConflict)
}

// GetPodAddressByNic reads one pod/NIC allocation from the transaction's
// snapshot so a caller can conditionally release the exact address it owns.
func (tx *Transaction) GetPodAddressByNic(podName, nicName, subnetName string) (string, string, string, bool, error) {
	if err := tx.ensureOpen(); err != nil {
		return "", "", "", false, err
	}
	subnet, err := tx.subnet(subnetName)
	if err != nil {
		return "", "", "", false, err
	}
	v4, v6, mac, found := subnet.getPodAddressByNic(podName, nicName)
	return v4, v6, mac, found, nil
}

// GetRandomAddressWithFamily stages a family-specific random allocation.
func (tx *Transaction) GetRandomAddressWithFamily(podName, nicName string, mac *string, subnetName, poolName, ipFamily string, skippedAddrs []string, checkConflict bool) (string, string, string, error) {
	if err := tx.ensureOpen(); err != nil {
		return "", "", "", err
	}
	if _, err := tx.subnet(subnetName); err != nil {
		return "", "", "", err
	}
	return getRandomAddressWithFamily(tx.subnets, podName, nicName, mac, subnetName, poolName, ipFamily, skippedAddrs, checkConflict)
}

// GetStaticAddress stages a static allocation or rebind.
func (tx *Transaction) GetStaticAddress(podName, nicName, ip string, mac *string, subnetName string, checkConflict bool) (string, string, string, error) {
	return tx.GetStaticAddressWithFamily(podName, nicName, ip, mac, subnetName, "", checkConflict)
}

// GetStaticAddressWithFamily stages a family-specific static allocation or
// rebind.
func (tx *Transaction) GetStaticAddressWithFamily(podName, nicName, ip string, mac *string, subnetName, ipFamily string, checkConflict bool) (string, string, string, error) {
	if err := tx.ensureOpen(); err != nil {
		return "", "", "", err
	}
	if _, err := tx.subnet(subnetName); err != nil {
		return "", "", "", err
	}
	return getStaticAddressWithFamily(tx.subnets, podName, nicName, ip, mac, subnetName, ipFamily, checkConflict)
}

// ReleaseAddressByPod stages release for one subnet, or all subnets when the
// subnet name is empty.
func (tx *Transaction) ReleaseAddressByPod(podName, subnetName string) error {
	if err := tx.ensureOpen(); err != nil {
		return err
	}
	if err := tx.cloneSubnets(subnetName); err != nil {
		return err
	}
	releaseAddressByPod(tx.subnets, podName, subnetName)
	return nil
}

// ReleaseAddressByNic stages release for one NIC on one subnet, or all
// subnets when the subnet name is empty.
func (tx *Transaction) ReleaseAddressByNic(podName, nicName, subnetName string) error {
	if err := tx.ensureOpen(); err != nil {
		return err
	}
	if err := tx.cloneSubnets(subnetName); err != nil {
		return err
	}
	releaseAddressByNic(tx.subnets, podName, nicName, subnetName)
	return nil
}

// ReleaseAddressByNicIfMatches stages release only when the current
// allocation still matches the expected address tuple. The lookup and
// release happen on the same transaction snapshot.
func (tx *Transaction) ReleaseAddressByNicIfMatches(podName, nicName, subnetName, expectedV4, expectedV6, expectedMAC string) error {
	if err := tx.ensureOpen(); err != nil {
		return err
	}
	v4, v6, mac, found, err := tx.GetPodAddressByNic(podName, nicName, subnetName)
	if err != nil {
		return err
	}
	if !found || v4 != expectedV4 || v6 != expectedV6 || mac != expectedMAC {
		return ErrAddressOwnershipChanged
	}
	return tx.ReleaseAddressByNic(podName, nicName, subnetName)
}

func (tx *Transaction) ensureOpen() error {
	if tx.closed {
		return ErrTransactionClosed
	}
	return nil
}

func (tx *Transaction) subnet(name string) (*Subnet, error) {
	if subnet, ok := tx.subnets[name]; ok {
		return subnet, nil
	}
	subnet, ok := tx.ipam.Subnets[name]
	if !ok {
		return nil, ErrNoSubnet
	}
	subnet.Mutex.RLock()
	clone := subnet.clone()
	subnet.Mutex.RUnlock()
	tx.subnets[name] = clone
	return clone, nil
}

func (tx *Transaction) cloneSubnets(name string) error {
	if name != "" {
		if _, ok := tx.ipam.Subnets[name]; !ok {
			return nil
		}
		_, err := tx.subnet(name)
		return err
	}
	for name := range tx.ipam.Subnets {
		if _, err := tx.subnet(name); err != nil {
			return err
		}
	}
	return nil
}

func (tx *Transaction) commit() {
	maps.Copy(tx.ipam.Subnets, tx.subnets)
	tx.closed = true
}

func (s *Subnet) clone() *Subnet {
	clone := &Subnet{
		Name:         s.Name,
		CIDR:         s.CIDR,
		Protocol:     s.Protocol,
		V4CIDR:       cloneIPNet(s.V4CIDR),
		V4Free:       s.V4Free.Clone(),
		V4Reserved:   s.V4Reserved.Clone(),
		V4Available:  s.V4Available.Clone(),
		V4Using:      s.V4Using.Clone(),
		V6CIDR:       cloneIPNet(s.V6CIDR),
		V6Free:       s.V6Free.Clone(),
		V6Reserved:   s.V6Reserved.Clone(),
		V6Available:  s.V6Available.Clone(),
		V6Using:      s.V6Using.Clone(),
		V4Gw:         s.V4Gw,
		V6Gw:         s.V6Gw,
		GatewayMAC:   s.GatewayMAC,
		V4NicToIP:    cloneIPMap(s.V4NicToIP),
		V4IPToPod:    cloneStringMap(s.V4IPToPod),
		V6NicToIP:    cloneIPMap(s.V6NicToIP),
		V6IPToPod:    cloneStringMap(s.V6IPToPod),
		NicToMac:     cloneStringMap(s.NicToMac),
		MacToPod:     cloneStringMap(s.MacToPod),
		PodToNicList: make(map[string][]string, len(s.PodToNicList)),
		IPPools:      make(map[string]*IPPool, len(s.IPPools)),
	}
	for pod, nics := range s.PodToNicList {
		clone.PodToNicList[pod] = slices.Clone(nics)
	}
	for name, pool := range s.IPPools {
		clone.IPPools[name] = pool.clone()
	}
	return clone
}

func (p *IPPool) clone() *IPPool {
	if p == nil {
		return nil
	}
	clone := *p
	clone.V4IPs = p.V4IPs.Clone()
	clone.V4Free = p.V4Free.Clone()
	clone.V4Available = p.V4Available.Clone()
	clone.V4Reserved = p.V4Reserved.Clone()
	clone.V4Released = p.V4Released.Clone()
	clone.V4Using = p.V4Using.Clone()
	clone.V6IPs = p.V6IPs.Clone()
	clone.V6Free = p.V6Free.Clone()
	clone.V6Available = p.V6Available.Clone()
	clone.V6Reserved = p.V6Reserved.Clone()
	clone.V6Released = p.V6Released.Clone()
	clone.V6Using = p.V6Using.Clone()
	return &clone
}

func cloneIPNet(cidr *net.IPNet) *net.IPNet {
	if cidr == nil {
		return nil
	}
	return &net.IPNet{IP: slices.Clone(cidr.IP), Mask: slices.Clone(cidr.Mask)}
}

func cloneIPMap(values map[string]IP) map[string]IP {
	clone := make(map[string]IP, len(values))
	for key, value := range values {
		clone[key] = value.Clone()
	}
	return clone
}

func cloneStringMap(values map[string]string) map[string]string {
	clone := make(map[string]string, len(values))
	maps.Copy(clone, values)
	return clone
}
