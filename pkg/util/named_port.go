package util

type NamedPortInfo struct {
	PortId int32
	Pods   map[string]string // pods named port belong to
}
