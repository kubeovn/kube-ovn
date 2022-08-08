package ovs

import (
	"context"
	"fmt"
	"strconv"

	"github.com/ovn-org/libovsdb/ovsdb"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

const (
	portGroupKey = "pg"
)

// GetAcl get acl by name
// aclName's format is: pgName.direction.priority
func (c OvnClient) GetAcl(aclName string, ignoreNotFound bool) (*ovnnb.ACL, error) {
	aclList := make([]ovnnb.ACL, 0)
	if err := c.ovnNbClient.WhereCache(func(acl *ovnnb.ACL) bool {
		return acl.Name != nil && *acl.Name == aclName
	}).List(context.TODO(), &aclList); err != nil {
		return nil, fmt.Errorf("get acl %s: %v", aclName, err)
	}

	// not found
	if len(aclList) == 0 {
		if ignoreNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("not found acl %s", aclName)
	}

	if len(aclList) > 1 {
		return nil, fmt.Errorf("more than one acl with same name %s", aclName)
	}

	return &aclList[0], nil
}

func (c OvnClient) AclExists(aclName string) (bool, error) {
	acl, err := c.GetAcl(aclName, true)
	return acl != nil, err
}

// CreateAclOp generate operation which create an ACL,
func (c OvnClient) CreateAclOp(acl *ovnnb.ACL) ([]ovsdb.Operation, error) {
	if acl.Name == nil {
		return nil, fmt.Errorf("acl name is empty")
	}

	aclName := *acl.Name

	// aclName's format is: pgName.direction.priority
	// the acl maximum allowed length is 63
	if len(aclName) > 63 {
		return nil, fmt.Errorf("acl %s length %d is greater than maximum allowed length 63", aclName, len(aclName))
	}

	exists, err := c.AclExists(aclName)
	if err != nil {
		return nil, err
	}

	// found, ingore
	if exists {
		return nil, nil
	}

	op, err := c.ovnNbClient.Create(acl)
	if err != nil {
		return nil, fmt.Errorf("generate operations for creating acl %s: %v", aclName, err)
	}

	return op, nil
}

// newAcl return acl with basic information
func (c OvnClient) newAcl(pgName, match, direction, action, priority string, options ...func(acl *ovnnb.ACL)) *ovnnb.ACL {
	// aclName's format is: pgName.direction.priority
	aclName := fmt.Sprintf("%s.%s.%s", pgName, direction, priority)

	intPriority, _ := strconv.Atoi(priority)

	acl := &ovnnb.ACL{
		Name:      &aclName,
		Action:    action,
		Direction: direction,
		Match:     match,
		Priority:  intPriority,
		ExternalIDs: map[string]string{
			portGroupKey: pgName,
		},
	}

	for _, option := range options {
		option(acl)
	}

	return acl
}
