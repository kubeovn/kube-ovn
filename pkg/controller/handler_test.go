package controller

import (
	"testing"

	"github.com/oilbeater/libovsdb"
)

func TestListSwitch(t *testing.T) {
	t.Skip()
	ovs, e := libovsdb.Connect("127.0.0.1", 6641)
	if e != nil {
		t.Error(e)
		return
	}
	oh := &OvnHandler{OvsClient: ovs}
	oh.handleListSwitch(nil, nil)
}
