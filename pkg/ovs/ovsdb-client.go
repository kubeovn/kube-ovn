package ovs

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

// OvsdbServerAddress constructs the ovsdb-server address based on the given host and port.
// It uses "ssl" scheme if the ENABLE_SSL environment variable is set to "true", otherwise "tcp".
//
// For example:
//
//	OvsdbServerAddress("localhost:6641") returns "tcp:localhost:6641" or "ssl:localhost:6641" based on the ENABLE_SSL setting.
func OvsdbServerAddress(host string, port intstr.IntOrString) string {
	scheme := "tcp"
	if os.Getenv(util.EnvSSLEnabled) == "true" {
		scheme = "ssl"
	}
	return fmt.Sprintf("%s:%s", scheme, net.JoinHostPort(host, port.String()))
}

// Query executes an ovsdb-client query command against the given address and database with the provided operations
// and returns the operation results.
// For SSL connections, it assumes the certificates are located at /var/run/tls/{key,cert,cacert}.
// The timeout is specified in seconds.
// For more details, see `ovsdb-client --help`.
//
// For example:
//
//	results, err := Query("tcp:[::1]:6641", "OVN_Northbound", 3, ovsdb.Operation{...})
//	results, err := Query("ssl:[::1]:6641", "OVN_Northbound", 3, ovsdb.Operation{...})
func Query(address, database string, timeout int, operations ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
	transArgs := ovsdb.NewTransactArgs(database, operations...)
	query, err := json.Marshal(transArgs)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ovsdb transaction args %+v: %w", transArgs, err)
	}

	args := []string{"--timeout", strconv.Itoa(timeout), "query", address, string(query)}
	if strings.HasPrefix(address, "ssl:") {
		args = slices.Insert(args, 0, CmdSSLArgs()...)
	}

	output, err := exec.Command("ovsdb-client", args...).CombinedOutput() // #nosec G204
	if err != nil {
		return nil, fmt.Errorf("failed to execute ovsdb-client with args %v: %w\noutput: %s", args, err, string(output))
	}

	var results []ovsdb.OperationResult
	if err = json.Unmarshal(output, &results); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ovsdb-client output %q: %w", string(output), err)
	}

	return results, nil
}
