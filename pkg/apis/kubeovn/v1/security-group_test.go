package v1

import (
	"errors"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func TestSecurityGroupProtocolSchema(t *testing.T) {
	file, err := os.Open("../../../../charts/kube-ovn-v2/crds/kube-ovn-crd.yaml")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	decoder := yaml.NewYAMLOrJSONDecoder(file, 4096)
	for {
		var crd apiextensionsv1.CustomResourceDefinition
		err := decoder.Decode(&crd)
		if errors.Is(err, io.EOF) {
			t.Fatal("SecurityGroup CRD not found")
		}
		require.NoError(t, err)
		if crd.Name != "security-groups.kubeovn.io" {
			continue
		}
		var schema apiextensions.JSONSchemaProps
		require.NoError(t, apiextensionsv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(crd.Spec.Versions[0].Schema.OpenAPIV3Schema, &schema, nil))
		validator, _, err := validation.NewSchemaValidator(&schema)
		require.NoError(t, err)
		for _, direction := range []string{"ingressRules", "egressRules"} {
			for _, tc := range []struct {
				name, protocol string
				omit, invalid  bool
			}{
				{name: "omitted", omit: true},
				{name: "empty"},
				{name: "all", protocol: "all"},
				{name: "icmp", protocol: "icmp"},
				{name: "tcp", protocol: "tcp"},
				{name: "udp", protocol: "udp"},
				{name: "gre", protocol: "gre", invalid: true},
				{name: "uppercase", protocol: "TCP", invalid: true},
				{name: "sctp", protocol: "sctp", invalid: true},
			} {
				t.Run(direction+"/"+tc.name, func(t *testing.T) {
					rule := map[string]any{"ipVersion": "ipv4", "remoteType": "address", "remoteAddress": "192.0.2.1", "policy": "allow"}
					if !tc.omit {
						rule["protocol"] = tc.protocol
					}
					object := map[string]any{"spec": map[string]any{direction: []any{rule}}}
					result := validator.Validate(object)
					if tc.invalid {
						require.False(t, result.IsValid())
					} else {
						require.True(t, result.IsValid(), "%v", result.Errors)
					}
				})
			}
		}
		return
	}
}
