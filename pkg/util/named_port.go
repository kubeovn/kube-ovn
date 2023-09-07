package util

import "github.com/scylladb/go-set/strset"

type NamedPortInfo struct {
	PortID int32
	Pods   *strset.Set
}
