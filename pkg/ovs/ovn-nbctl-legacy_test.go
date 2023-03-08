package ovs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_parseLrRouteListOutput(t *testing.T) {
	ast := assert.New(t)
	output := `IPv4 Routes
                10.42.1.1            169.254.100.45 dst-ip (learned)
                10.42.1.3                100.64.0.2 dst-ip
                10.16.0.2                100.64.0.2 src-ip
             10.17.0.0/16            169.254.100.45 dst-ip (learned)
            100.65.0.0/16            169.254.100.45 dst-ip (learned)`
	routeList, err := parseLrRouteListOutput(output)
	ast.Nil(err)
	ast.Equal(2, len(routeList))

	output = `IPv6 Routes
    fc00:f853:ccd:e793::2            fd00:100:64::2 dst-ip
    fc00:f853:ccd:e793::3            fd00:100:64::3 dst-ip
    fc00:f853:ccd:e793::4            fd00:100:64::4444 dst-ip
            fd00:10:16::2            fd00:100:64::3 src-ip
            fd00:10:16::d            fd00:100:64::2 src-ip
         fd00:11:15::/112            fd00:100:64::2 src-ip ecmp
         fd00:11:15::/112            fd00:100:64::3 src-ip ecmp`
	routeList, err = parseLrRouteListOutput(output)
	ast.Nil(err)
	ast.Equal(7, len(routeList))
}

func Test_parseLrPolicyRouteListOutput(t *testing.T) {
	t.SkipNow()
	ast := assert.New(t)
	output := `        
		10                              ip4.src == 1.1.0.0/24         reroute                198.19.0.4
        10     ip4.src == 1.1.0.0/24 || ip4.src == 1.1.4.0/24         reroute                198.19.0.4
        10 ip4.src == 1.1.0.0/24 || ip4.src == 1.1.4.0/24 || Iip4.src ==1.1.5.0/24         reroute                198.19.0.4
        10                              ip4.src == 1.1.1.0/24            drop`
	routeList, err := parseLrPolicyRouteListOutput(output)
	ast.Nil(err)
	ast.Equal(6, len(routeList))
}
