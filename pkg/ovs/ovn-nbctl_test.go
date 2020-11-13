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
	ast.Equal(5, len(routeList))
}
