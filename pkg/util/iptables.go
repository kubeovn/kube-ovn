package util

type IPTableRule struct {
	Table string
	Chain string
	Rule  []string
}
