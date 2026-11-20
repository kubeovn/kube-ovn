package ha

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/kind"
)

func runZeroTailDBCommand(f *framework.Framework, pod corev1.Pod, command string) string {
	ginkgo.GinkgoHelper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	stdout, stderr, err := framework.ExecShellInContainerContext(ctx, f, pod.Namespace, pod.Name, "ovn-central", command)
	framework.ExpectNoError(err, "database command on %s: %s; stdout=%s stderr=%s", pod.Name, command, stdout, stderr)
	return strings.TrimSpace(stdout)
}

func writeZeroTailMarker(f *framework.Framework, pods []corev1.Pod, db, key, value string) {
	ginkgo.GinkgoHelper()
	for _, pod := range pods {
		if pod.Labels["ovn-"+db+"-leader"] == "true" {
			command := fmt.Sprintf("ovn-%sctl --timeout=10 set %s_Global . external_ids:%s=%s", db, strings.ToUpper(db), key, value)
			runZeroTailDBCommand(f, pod, command)
			return
		}
	}
	framework.Failf("no ovn-%s leader available for marker write", db)
}

func verifyZeroTailMarker(f *framework.Framework, pods []corev1.Pod, db, key, value string) {
	ginkgo.GinkgoHelper()
	for _, pod := range pods {
		framework.WaitUntil(time.Second, 30*time.Second, func(ctx context.Context) (bool, error) {
			command := fmt.Sprintf("ovn-%sctl --timeout=5 --no-leader-only get %s_Global . external_ids:%s", db, strings.ToUpper(db), key)
			stdout, stderr, err := framework.ExecShellInContainerContext(ctx, f, pod.Namespace, pod.Name, "ovn-central", command)
			if err != nil {
				framework.Logf("marker read on %s failed: %v; stdout=%s stderr=%s", pod.Name, err, stdout, stderr)
				return false, nil
			}
			return strings.Trim(strings.TrimSpace(stdout), "\"") == value, nil
		}, "committed marker on "+pod.Name)
	}
}

func stopZeroTailCentral(client *framework.DeploymentClient, deploy *appsv1.Deployment) {
	ginkgo.GinkgoHelper()
	setCentralReplicas(client, deploy.Name, 0)
	framework.WaitUntil(time.Second, time.Minute, func(ctx context.Context) (bool, error) {
		pods, err := client.GetAllPodsContext(ctx, deploy)
		return err == nil && len(pods.Items) == 0, err
	}, "all central processes to stop before changing database files")
}

type zeroTailNode interface {
	Name() string
	Exec(...string) ([]byte, []byte, error)
}

type zeroTailMember struct {
	node     zeroTailNode
	backup   string
	inode    string
	identity string
	backedUp bool
}

func cleanupZeroTailFiles(members map[string]*zeroTailMember, path string, restore bool) error {
	var failures []error
	if restore {
		for _, member := range members {
			if member.backedUp {
				stdout, stderr, err := member.node.Exec("cp", member.backup, path)
				if err != nil {
					failures = append(failures, fmt.Errorf("restore %s: %w; stdout=%s stderr=%s", member.node.Name(), err, stdout, stderr))
				}
			}
		}
		// A partial ensemble restore needs every backup for another attempt.
		if len(failures) != 0 {
			return errors.Join(failures...)
		}
	}
	for _, member := range members {
		stdout, stderr, err := member.node.Exec("rm", "-f", member.backup)
		if err != nil {
			failures = append(failures, fmt.Errorf("remove backup on %s: %w; stdout=%s stderr=%s", member.node.Name(), err, stdout, stderr))
		}
	}
	return errors.Join(failures...)
}

// injectZeroTails changes only stopped, disposable Kind members. Failed specs
// restore the saved ensemble after diagnostics have captured the failed state.
func injectZeroTails(f *framework.Framework, deploy *appsv1.Deployment, pods []corev1.Pod, nodes map[string]*kind.Node, db string) func([]corev1.Pod) {
	ginkgo.GinkgoHelper()
	client := f.DeploymentClientNS(deploy.Namespace)
	path := dbFileHostPath(db)
	suffix := ".zero-tail-e2e-" + framework.RandomSuffix()
	members := make(map[string]*zeroTailMember, len(pods))
	for _, pod := range pods {
		framework.ExpectHaveKey(nodes, pod.Spec.NodeName)
		framework.ExpectEqual(members[pod.Spec.NodeName] == nil, true, "central members must use distinct nodes")
		identity := runZeroTailDBCommand(f, pod, fmt.Sprintf("ovsdb-tool db-cid %q && ovsdb-tool db-sid %q", dbFilePath(db), dbFilePath(db)))
		framework.ExpectHaveLen(strings.Fields(identity), 2)
		members[pod.Spec.NodeName] = &zeroTailMember{node: nodes[pod.Spec.NodeName], backup: path + suffix, identity: identity}
	}
	recovered := false
	ginkgo.DeferCleanup(func() {
		// Always attempt to restore the replica count, including when stopping
		// the deployment or restoring/removing one member's files fails.
		defer func() {
			setCentralReplicas(client, deploy.Name, *deploy.Spec.Replicas)
			client.RolloutStatus(deploy.Name)
			waitForCentralLeaders(f)
		}()
		if !recovered {
			stopZeroTailCentral(client, deploy)
		}
		framework.ExpectNoError(cleanupZeroTailFiles(members, path, !recovered))
	})

	ginkgo.By("Stopping every central member so no live quorum can rebuild a damaged peer")
	stopZeroTailCentral(client, deploy)
	for _, member := range members {
		stdout, stderr, err := member.node.Exec("cp", path, member.backup)
		framework.ExpectNoError(err, "back up %s: stdout=%s stderr=%s", member.node.Name(), stdout, stderr)
		member.backedUp = true
		stdout, stderr, err = member.node.Exec("stat", "--format=%i", path)
		framework.ExpectNoError(err, "stat database: stdout=%s stderr=%s", stdout, stderr)
		member.inode = strings.TrimSpace(string(stdout))
		command := fmt.Sprintf("dd if=/dev/zero bs=80 count=1 oflag=append conv=notrunc status=none of=%q", path)
		stdout, stderr, err = member.node.Exec("bash", "-ec", command)
		framework.ExpectNoError(err, "append zero tail: stdout=%s stderr=%s", stdout, stderr)
	}
	setCentralReplicas(client, deploy.Name, *deploy.Spec.Replicas)
	client.RolloutStatus(deploy.Name)
	return func(current []corev1.Pod) {
		framework.ExpectHaveLen(current, len(members))
		for _, pod := range current {
			framework.ExpectHaveKey(members, pod.Spec.NodeName)
			member := members[pod.Spec.NodeName]
			identity := runZeroTailDBCommand(f, pod, fmt.Sprintf("ovsdb-tool db-cid %q && ovsdb-tool db-sid %q", dbFilePath(db), dbFilePath(db)))
			framework.ExpectEqual(identity, member.identity, "Raft cluster and server identity must survive")
			stdout, stderr, err := member.node.Exec("stat", "--format=%i", path)
			framework.ExpectNoError(err, "stat recovered database: stdout=%s stderr=%s", stdout, stderr)
			framework.ExpectEqual(strings.TrimSpace(string(stdout)), member.inode, "startup must preserve the original member file")
		}
		recovered = true
	}
}

func verifyZeroTailConnectivity(f *framework.Framework, pods []corev1.Pod) {
	ginkgo.GinkgoHelper()
	client := f.PodClientNS(corev1.NamespaceDefault)
	var probes []*corev1.Pod
	for i := range 2 {
		name := "ha-zero-tail-" + framework.RandomSuffix()
		pod := framework.MakePod(corev1.NamespaceDefault, name, nil, nil, framework.AgnhostImage, nil, []string{"netexec", "--http-port=8080"})
		pod.Spec.NodeName = pods[i].Spec.NodeName
		ginkgo.DeferCleanup(func() { client.DeleteSync(name) })
		probes = append(probes, client.CreateSync(pod))
	}
	framework.ExpectNotEmpty(probes[1].Status.PodIPs)
	for _, ip := range probes[1].Status.PodIPs {
		framework.WaitUntil(time.Second, 30*time.Second, func(ctx context.Context) (bool, error) {
			stdout, stderr, err := framework.ExecCommandInContainerContext(ctx, f, probes[0].Namespace, probes[0].Name, "container", "/agnhost", "connect", net.JoinHostPort(ip.IP, "8080"), "--protocol=tcp", "--timeout=5s")
			if err != nil {
				framework.Logf("cross-node connection to %s failed: %v; stdout=%s stderr=%s", ip.IP, err, stdout, stderr)
			}
			return err == nil, nil
		}, "new cross-node connection to "+ip.IP)
	}
}

func registerZeroTailHA(f *framework.Framework, nodes map[string]*kind.Node) {
	for _, db := range []string{"nb", "sb"} {
		framework.DisruptiveIt("ovn "+db+" db should recover zero-filled tails on all members without a live quorum", func() {
			f.SkipVersionPriorTo(1, 15, "Zero-filled Raft tail recovery is supported from v1.15")
			client := f.DeploymentClientNS(framework.KubeOvnNamespace)
			deploy := getCentralDeployment(client)
			if *deploy.Spec.Replicas < 3 {
				ginkgo.Skip("all-member zero-tail recovery requires at least three central replicas")
			}
			client.RolloutStatus(deploy.Name)
			waitForCentralLeaders(f)
			pods, err := getCentralPods(client, deploy)
			framework.ExpectNoError(err)
			framework.ExpectHaveLen(pods.Items, int(*deploy.Spec.Replicas))
			key, value := "ha-zero-tail-"+framework.RandomSuffix(), framework.RandomSuffix()
			writeZeroTailMarker(f, pods.Items, db, key, value)
			verifyZeroTailMarker(f, pods.Items, db, key, value)
			ginkgo.DeferCleanup(func() {
				current, err := getCentralPods(client, deploy)
				framework.ExpectNoError(err)
				for _, pod := range current.Items {
					if pod.Labels["ovn-"+db+"-leader"] == "true" {
						runZeroTailDBCommand(f, pod, fmt.Sprintf("ovn-%sctl --timeout=10 remove %s_Global . external_ids %s", db, strings.ToUpper(db), key))
						return
					}
				}
				framework.Failf("no leader available to remove marker")
			})

			verifyIdentity := injectZeroTails(f, deploy, pods.Items, nodes, db)
			pods, err = getCentralPods(client, deploy)
			framework.ExpectNoError(err)
			waitForDbRecovery(f, pods.Items)
			waitForCentralLeaders(f)
			verifyZeroTailMarker(f, pods.Items, db, key, value)
			ginkgo.By("Committing and replicating a fresh write after recovery")
			pods, err = getCentralPods(client, deploy)
			framework.ExpectNoError(err)
			value = framework.RandomSuffix()
			writeZeroTailMarker(f, pods.Items, db, key, value)
			verifyZeroTailMarker(f, pods.Items, db, key, value)
			verifyZeroTailConnectivity(f, pods.Items)
			verifyIdentity(pods.Items)
		})
	}
}
