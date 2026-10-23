package framework

import "github.com/kubeovn/kube-ovn/test/e2e/framework/kubectlko"

// KubectlKoArgs selects the syntax of the release under test. Compatibility is
// confined to E2E fixtures; the Go plugin itself has no legacy command aliases.
func KubectlKoArgs(args ...string) []string {
	f := &Framework{}
	f.parseEnv()
	if f.VersionPriorTo(1, 17) {
		return args
	}
	return kubectlko.Args(args...)
}
