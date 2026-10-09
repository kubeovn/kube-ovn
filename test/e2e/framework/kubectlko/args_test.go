package kubectlko

import (
	"reflect"
	"testing"
)

func TestFixtureArguments(t *testing.T) {
	cases := []struct{ legacy, current []string }{
		{[]string{"ko", "nbctl", "--", "ls-add", "with space"}, []string{"ko", "exec", "nbctl", "--", "--", "ls-add", "with space"}},
		{[]string{"ko", "icnbctl", "show"}, []string{"ko", "exec", "ic-nbctl", "--", "show"}},
		{[]string{"ko", "ofctl", "worker", "dump-flows", "br-int"}, []string{"ko", "exec", "ofctl", "--node", "worker", "--", "dump-flows", "br-int"}},
		{[]string{"ko", "sb", "backup"}, []string{"ko", "db", "sb", "backup"}},
		{[]string{"ko", "nb", "dbstatus"}, []string{"ko", "db", "health"}},
		{[]string{"ko", "tcpdump", "app/web", "-w", "-"}, []string{"ko", "capture", "--pod", "app/web", "--", "-w", "-"}},
		{[]string{"ko", "ovn-trace", "app/web", "2001:db8::1", "tcp", "443"}, []string{"ko", "trace", "--pod", "app/web", "--dst-ip", "2001:db8::1", "--engine", "ovn", "--protocol", "tcp", "--dst-port", "443"}},
		{[]string{"ko", "trace", "node//worker", "192.0.2.1", "00:11:22:33:44:55", "arp", "reply"}, []string{"ko", "trace", "--node", "worker", "--dst-ip", "192.0.2.1", "--dst-mac", "00:11:22:33:44:55", "--protocol", "arp", "--arp-op", "reply"}},
		{[]string{"ko", "diagnose", "IPPorts", "tcp-192.0.2.1-443,udp-2001:db8::1-53"}, []string{"ko", "diagnose", "connectivity", "--target", "tcp://192.0.2.1:443", "--target", "udp://[2001:db8::1]:53"}},
		{[]string{"ko", "diagnose", "all"}, []string{"ko", "diagnose", "cluster"}},
		{[]string{"ko", "diagnose", "subnet", "ovn-default"}, []string{"ko", "diagnose", "subnet", "ovn-default"}},
		{[]string{"ko", "log", "ovs"}, []string{"ko", "logs", "--component", "ovs"}},
	}
	for _, tc := range cases {
		if got := Args(tc.legacy...); !reflect.DeepEqual(got, tc.current) {
			t.Errorf("%q: got %q, want %q", tc.legacy, got, tc.current)
		}
	}
}
