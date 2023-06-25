package v1

import (
	"encoding/json"
	"fmt"

	"k8s.io/klog/v2"
)

func (s *IPPoolStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}

func (ss *SubnetStatus) Bytes() ([]byte, error) {
	// {"availableIPs":65527,"usingIPs":9} => {"status": {"availableIPs":65527,"usingIPs":9}}
	bytes, err := json.Marshal(ss)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}

func (vs *VpcStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(vs)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}

func (sgs *SecurityGroupStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(sgs)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}

func (vipst *VipStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(vipst)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}

func (ieips *IptablesEipStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(ieips)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}

func (ifips *IptablesFIPRuleStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(ifips)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}

func (idnats *IptablesDnatRuleStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(idnats)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}

func (isnats *IptablesSnatRuleStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(isnats)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}

func (oeips *OvnEipStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(oeips)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}

func (ofs *OvnFipStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(ofs)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}

func (osrs *OvnSnatRuleStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(osrs)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}

func (odrs *OvnDnatRuleStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(odrs)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}

func (qoss *QoSPolicyStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(qoss)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}

func (vns *VpcNatStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(vns)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}
