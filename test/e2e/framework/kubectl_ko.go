package framework

import "github.com/kubeovn/kube-ovn/test/e2e/framework/kubectlko"

// KubectlKoArgs selects the syntax of the release under test. Compatibility is
// confined to E2E fixtures; the Go plugin itself has no legacy command aliases.
func KubectlKoArgs(args ...string) []string {
	if KubectlKoUsesLegacySyntax() {
		return args
	}
	return kubectlko.Args(args...)
}

// KubectlKoUsesLegacySyntax reports whether the image under test still ships
// the pre-1.17 shell plugin.
func KubectlKoUsesLegacySyntax() bool {
	f := &Framework{}
	f.parseEnv()
	return f.VersionPriorTo(1, 17)
}
