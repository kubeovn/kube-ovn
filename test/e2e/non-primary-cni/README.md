# Non-Primary CNI E2E Tests

This directory contains end-to-end tests for Kube-OVN operating in non-primary CNI mode, where Kube-OVN works alongside another primary CNI plugin through Multus.

## Dynamic KIND Bridge Network Detection

🚀 **NEW FEATURE**: The tests now support **automatic KIND bridge network detection**!

The test suite automatically detects your KIND cluster's bridge network configuration and dynamically updates test configurations. This ensures tests work regardless of which Docker bridge CIDR your KIND cluster uses (172.17.0.0/16, 172.18.0.0/16, 172.19.0.0/16, etc.).

### How It Works
1. **Detection**: Tests automatically inspect the KIND Docker network using `docker.NetworkInspect(kind.NetworkName)`
2. **Processing**: Configuration files are dynamically updated with the correct CIDR and gateway
3. **Execution**: Tests run with the actual KIND bridge network settings
4. **Cleanup**: Temporary configuration files are automatically removed

### Benefits
- ✅ **Automatic**: No manual CIDR configuration required
- ✅ **Flexible**: Works with any KIND bridge network range  
- ✅ **Reliable**: Always uses the correct network configuration
- ✅ **Compatible**: Follows existing Kube-OVN test patterns

## Test Scenarios

The test suite covers three main scenarios with enhanced interface handling and dynamic network detection:

### 1. VPC Simple
- **Purpose**: Tests basic non-primary CNI functionality in VPC mode
- **Coverage**: Pod creation, network attachment, IP assignment validation, inter-pod connectivity
- **Interface**: Uses `net1` interface for connectivity tests
- **Config**: Requires `VPC/00-vpc-simple.yaml` test configuration

### 2. VPC NAT Gateway
- **Purpose**: Tests non-primary CNI with VPC NAT Gateway functionality
- **Coverage**: SNAT/DNAT rules, external connectivity, NAT gateway operations, EIP management
- **Interface**: Uses `net2` interface for external connectivity tests, `net1` for internal connectivity
- **Features**: 
  - SNAT external connectivity testing (8.8.8.8 via net2)
  - DNAT rule verification and mapping validation
  - EIP allocation and association testing
  - Pod-to-pod communication within VPC
- **Config**: Requires `VPC/01-vpc-nat-gw.yaml` test configuration

### 3. Logical Network Simple
- **Purpose**: Tests non-primary CNI with logical network isolation
- **Coverage**: Network isolation, inter-pod communication, logical switches
- **Interface**: Uses `net1` interface for connectivity tests
- **Config**: Requires `LogicalNetwork/00-lnet-simple.yaml` test configuration

## Prerequisites

1. **KIND Cluster**: Kubernetes cluster deployed using KIND with Multus CNI installed
2. **Primary CNI**: Another CNI plugin (e.g., Flannel, Calico) running as primary
3. **Kube-OVN**: Deployed in non-primary CNI mode (`KUBE_OVN_PRIMARY_CNI=false`)
4. **Test Configs**: YAML configuration files in `TEST_CONFIG_PATH` directory

## Configuration Files

Test configuration files should be placed in `/opt/testconfigs/` (default) or specified via `TEST_CONFIG_PATH`:

```
/opt/testconfigs/
├── VPC/
│   ├── 00-vpc-simple.yaml
│   └── 01-vpc-nat-gw.yaml
└── LogicalNetwork/
    └── 00-lnet-simple.yaml
```

Each configuration file should contain NetworkAttachmentDefinition and other required resources with staged deployment using `config-stage` labels (0, 1, 2).

## Running Tests

### Run All Non-Primary CNI Tests
```bash
make kube-ovn-non-primary-cni-e2e
```

### Run Individual Test Suites

#### VPC Simple Tests
```bash
make kube-ovn-non-primary-cni-vpc-simple-e2e
```

#### VPC NAT Gateway Tests
```bash
make kube-ovn-non-primary-cni-vpc-nat-gw-e2e
```

#### Logical Network Simple Tests
```bash
make kube-ovn-non-primary-cni-lnet-simple-e2e
```

## Environment Variables

- `E2E_BRANCH`: Git branch being tested (default: current branch)
- `E2E_IP_FAMILY`: IP family for tests (IPv4/IPv6/DualStack)
- `E2E_NETWORK_MODE`: Network mode (overlay/underlay)
- `TEST_CONFIG_PATH`: Path to test configuration files (default: `/opt/testconfigs`)
- `KUBE_OVN_PRIMARY_CNI`: Should be `false` for non-primary CNI tests

## Interface Configuration

The tests support configurable network interfaces:

- **Default Interface**: `net1` (defined as `DefaultNetworkInterface` constant)
- **External Connectivity**: `net2` used for SNAT external connectivity tests
- **Interface Detection**: Automatic interface name extraction from network status annotations
- **Flexible IP Parsing**: Enhanced IP extraction logic that works with different interface names

**Note**: These tests are designed exclusively for KIND cluster deployments.

## Test Structure

The tests use the Ginkgo BDD framework and follow these patterns:

1. **Setup**: Initialize Kubernetes clients and load test configurations
2. **Staged Deployment**: Apply resources in stages using `config-stage` labels
   - Stage 0: Infrastructure (VPCs, subnets, network attachment definitions)
   - Stage 1: NAT Gateway, EIPs, SNAT/DNAT rules
   - Stage 2: Test pods
3. **IP Discovery**: Extract pod IPs from network status annotations with configurable interface support
4. **Connectivity Testing**: Test network connectivity with specific interface binding
5. **Validation**: Verify network attachments, IP assignments, NAT rules, and connectivity
6. **Cleanup**: Remove test resources after completion

### Key Helper Functions

- `getPodNonPrimaryIP(pod, interfaceName)`: Extracts IP from network status annotation for specified interface
- `testPodConnectivity(pod, targetIP, description)`: Tests connectivity using default interface
- `testPodConnectivityWithInterface(pod, targetIP, description, interface)`: Tests connectivity using specified interface
- `verifyDNATRule(rule, expectedEIP, expectedInternalIP)`: Validates DNAT rule configuration

## Debugging

### Verbose Output
Use Ginkgo's verbose flag for detailed test output:
```bash
GINKGO_OUTPUT_OPT="-v" make kube-ovn-non-primary-cni-e2e
```

### Focus on Specific Tests
Use Ginkgo's focus pattern to run specific test cases:
```bash
# Focus on connectivity tests
ginkgo --focus="connectivity" ./test/e2e/non-primary-cni/

# Focus on specific feature
ginkgo --focus="Feature:VPC-Simple" ./test/e2e/non-primary-cni/
```

### Test Logs
Test logs and artifacts are typically stored in:
- Container logs: `kubectl logs -n kube-system <pod-name>`
- Test output: Ginkgo generates detailed test reports
- Resource dumps: Use `kubectl describe` for resource inspection

## Common Issues

1. **Missing NetworkAttachmentDefinition**: Ensure NAD resources are created in the correct namespace
2. **IP Assignment Failures**: Check IPAM configuration and available IP ranges
3. **Connectivity Issues**: Verify OVN/OVS configuration and network policies
4. **Test Config Not Found**: Ensure test configuration files exist in `TEST_CONFIG_PATH`
5. **Interface Not Found**: Verify network interface names match those in test configurations
6. **Network Status Annotation Missing**: Check that pods have proper network status annotations from CNI
7. **SNAT/DNAT Rule Issues**: Verify NAT Gateway configuration and EIP allocation
8. **Wrong Interface for Connectivity**: Ensure correct interface is specified for different test scenarios

## Recent Improvements

### v2024.09 Enhancements
- ✨ **Dynamic KIND Bridge Detection**: Automatic detection and configuration of KIND cluster bridge networks
- **Configurable Interface Support**: Tests now support different network interfaces per test case
- **Enhanced IP Parsing**: Improved IP extraction from network status annotations  
- **Interface-Specific Connectivity**: SNAT tests use `net2` interface for external connectivity
- **Flexible Interface Detection**: Interface names are now passed as parameters rather than hardcoded
- **Better Error Handling**: Enhanced error messages for interface and connectivity issues

### Technical Implementation
The dynamic detection implementation follows established Kube-OVN patterns:

```go
// Detect KIND bridge network
network, err := docker.NetworkInspect(kind.NetworkName)
kindNetwork := &KindBridgeNetwork{
    CIDR:    config.Subnet,
    Gateway: config.Gateway,
}

// Process configuration with detected network
yamlFile, err := processConfigWithKindBridge(originalYamlFile, kindNetwork)
```

### Validation Scripts
- **`scripts/detect-kind-bridge.sh`**: Manual KIND bridge network detection
- **`scripts/test-dynamic-detection.sh`**: Validation of dynamic detection implementation

## Integration with CI/CD

These tests are designed to integrate with existing Kube-OVN CI/CD pipelines:

1. **Build Phase**: `ginkgo build` compiles the test binary
2. **Execution Phase**: Tests run on KIND clusters with appropriate timeout settings
3. **Reporting Phase**: Ginkgo generates XML/JSON reports for CI systems

## Contributing

When adding new test cases:

1. Follow existing test patterns and naming conventions
2. Add appropriate cleanup logic in `AfterEach` blocks
3. Use meaningful test descriptions and context blocks
4. Update this README with new test scenarios
5. Ensure tests work in KIND environments with Multus CNI
