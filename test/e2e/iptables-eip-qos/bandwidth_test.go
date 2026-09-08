package ovn_eip

import (
	"reflect"
	"testing"
)

func TestParseBandwidthFromIperfOutput(t *testing.T) {
	output := `time,srcaddress,srcport,dstaddr,dstport,transferid,istart,iend,bytes,speed,writecnt,writeerr,tcpretry,tcpcwnd,tcppcwnd,tcprtt,tcprttvar
+0000:20260908083114.550,10.0.0.2,37818,172.23.0.12,20288,1,0.0,1.0,57278528,458228224,-1,-1,4294967295,-1,4294967295,0,0
+0000:20260908083122.763,10.0.0.2,37818,172.23.0.12,20288,1,0.0,10.2,110755904,86754824,-1,-1,4294967295,-1,4294967295,0,0`

	got := parseBandwidthFromIperfOutput(output)
	want := []float64{458228224, 86754824}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseBandwidthFromIperfOutput() = %v, want %v", got, want)
	}
}
