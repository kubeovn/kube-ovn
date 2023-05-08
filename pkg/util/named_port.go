package util

import "github.com/scylladb/go-set/strset"

type NamedPortInfo struct {
	PortId int32
	Pods   *strset.Set
}
