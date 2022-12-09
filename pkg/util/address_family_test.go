package util

import (
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func TestProtocolToFamily(t *testing.T) {
	tests := []struct {
		name string
		prot string
		want int
		err  string
	}{
		{
			name: "correct",
			prot: kubeovnv1.ProtocolIPv4,
			want: unix.AF_INET,
			err:  "",
		},
		{
			name: "v6",
			prot: kubeovnv1.ProtocolIPv6,
			want: unix.AF_INET6,
			err:  "",
		},
		{
			name: "dual",
			prot: kubeovnv1.ProtocolDual,
			want: unix.AF_UNSPEC,
			err:  "",
		},
		{
			name: "err",
			prot: "damn",
			want: -1,
			err:  "invalid protocol",
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			ans, err := ProtocolToFamily(c.prot)
			if ans != c.want || !ErrorContains(err, c.err) {
				t.Errorf("%v expected %v, %v, but %v, %v got",
					c.prot, c.want, c.err, ans, err)
			}
		})
	}
}

func ErrorContains(out error, want string) bool {
	if out == nil {
		return want == ""
	}
	if want == "" {
		return false
	}
	return strings.Contains(out.Error(), want)
}
