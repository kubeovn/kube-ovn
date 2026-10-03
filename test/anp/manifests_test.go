package anp

import (
	"net/url"
	"testing"
)

func TestConformanceManifestsURL(t *testing.T) {
	for _, ref := range []string{"v0.1.8", "3910463a5686"} {
		t.Run(ref, func(t *testing.T) {
			manifest, err := conformanceManifestsURL(ref)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := url.Parse(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if parsed.Scheme != "https" || parsed.Host != "raw.githubusercontent.com" || parsed.Path != "/kubernetes-sigs/network-policy-api/"+ref+"/conformance/base/manifests.yaml" {
				t.Fatalf("manifest must be an absolute HTTPS URL for the pinned source: %s", manifest)
			}
		})
	}
}
