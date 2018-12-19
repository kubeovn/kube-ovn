package controller

import (
	"github.com/oilbeater/libovsdb"
	"testing"
)

func TestListSwitch(t *testing.T) {
	ovs, e := libovsdb.Connect("127.0.0.1", 6641)
	if e != nil {
		t.Error(e)
		return
	}
	oh := &OvnHandler{OvsClient: ovs}
	oh.handleListSwitch(nil, nil)
}
