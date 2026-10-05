package ko

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/exec"
)

type recoveryRecord struct {
	ID         string   `json:"id"`
	Namespace  string   `json:"namespace"`
	SourceNode string   `json:"sourceNode"`
	Replicas   int32    `json:"replicas"`
	Directory  string   `json:"remoteBackupDirectory"`
	Stage      string   `json:"stage"`
	Targets    []Target `json:"targets"`
}

func (a *Application) addRestoreCommand(parent *cobra.Command) {
	var sourceNode string
	var dryRun, yes bool
	command := &cobra.Command{Use: "restore --source-node NODE --yes", Short: "Rebuild central databases from an existing node NB database", Args: cobra.NoArgs}
	command.Flags().StringVar(&sourceNode, "source-node", "", "Node whose existing northbound database is the recovery source")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "Print the verified recovery plan without stopping databases")
	command.Flags().BoolVar(&yes, "yes", false, "Confirm stopping central and rebuilding its NB/SB database cluster")
	command.Args = func(cmd *cobra.Command, args []string) error {
		if err := cobra.NoArgs(cmd, args); err != nil {
			return err
		}
		if sourceNode == "" || !yes && !dryRun {
			return errors.New("restore requires --source-node and --yes (or --dry-run)")
		}
		return validateResourceName("node", sourceNode)
	}
	command.RunE = a.run(func(ctx context.Context, client *Client, _ []string) error {
		deployment, record, err := client.planRecovery(ctx, sourceNode)
		if err != nil {
			return err
		}
		if dryRun {
			return json.MarshalWrite(a.streams.Out, record)
		}
		return a.restore(ctx, client, deployment, record)
	})
	parent.AddCommand(command)
}

func hostDatabasePath(spec corev1.PodSpec, containerName string) (string, error) {
	for _, container := range spec.Containers {
		if container.Name != containerName {
			continue
		}
		for _, mount := range container.VolumeMounts {
			if mount.MountPath != "/etc/ovn" || mount.ReadOnly || mount.SubPath != "" || mount.SubPathExpr != "" {
				continue
			}
			for _, volume := range spec.Volumes {
				if volume.Name == mount.Name && volume.HostPath != nil {
					return volume.HostPath.Path, nil
				}
			}
		}
	}
	return "", fmt.Errorf("%s must mount a writable hostPath at /etc/ovn without subPath", containerName)
}

func (c *Client) planRecovery(ctx context.Context, source string) (*appsv1.Deployment, *recoveryRecord, error) {
	deployment, err := c.Kubernetes.AppsV1().Deployments(c.Namespace).Get(ctx, "ovn-central", metav1.GetOptions{})
	if err != nil {
		return nil, nil, err
	}
	hostPath, err := hostDatabasePath(deployment.Spec.Template.Spec, "ovn-central")
	if err != nil {
		return nil, nil, err
	}
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas == 0 {
		return nil, nil, errors.New("central must have a known nonzero desired replica count; restore it before planning recovery")
	}
	nodes, err := c.recoveryNodes(ctx, deployment)
	if err != nil {
		return nil, nil, err
	}
	if !slices.Contains(nodes, source) {
		return nil, nil, fmt.Errorf("source node %s is not a central database member", source)
	}
	// start-db.sh bootstraps from the first NODE_IPS address. Placing a standalone
	// database only on another member would let that first member create an empty DB.
	if nodes[0] != source {
		return nil, nil, fmt.Errorf("recovery source must be bootstrap node %s (the first NODE_IPS member)", nodes[0])
	}
	record := &recoveryRecord{ID: runID(), Namespace: c.Namespace, SourceNode: source, Replicas: *deployment.Spec.Replicas, Stage: "planned"}
	// start-db.sh chmods /etc/ovn/* to 600; keep retained directories outside that glob.
	record.Directory = "/etc/ovn/.kubectl-ko-recovery-" + record.ID
	for _, node := range nodes {
		target, err := c.nodeTarget(ctx, node, "ovs")
		if err != nil {
			return nil, nil, err
		}
		pod, err := c.Kubernetes.CoreV1().Pods(c.Namespace).Get(ctx, target.Pod, metav1.GetOptions{})
		if err != nil {
			return nil, nil, err
		}
		ovsPath, mountErr := hostDatabasePath(pod.Spec, target.Container)
		switch {
		case mountErr != nil:
			// Helm OVS pods do not mount central's database directory. Plan a
			// temporary helper instead of touching the container's own filesystem.
			target = Target{Namespace: c.Namespace, Pod: fmt.Sprintf("ko-recovery-%s-%d", record.ID, len(record.Targets)), Container: "recovery", Node: node}
		case ovsPath != hostPath:
			return nil, nil, fmt.Errorf("database hostPath differs between central and OVS on %s", node)
		default:
			for _, role := range []string{"nb", "sb"} {
				if _, err := c.capture(ctx, target, "test", "-f", "/etc/ovn/ovn"+role+"_db.db"); err != nil {
					return nil, nil, err
				}
			}
		}
		record.Targets = append(record.Targets, target)
	}
	return deployment, record, nil
}

func (r *resourceRun) prepareRecoveryTargets(ctx context.Context, deployment *appsv1.Deployment, record *recoveryRecord) error {
	hostPath, err := hostDatabasePath(deployment.Spec.Template.Spec, "ovn-central")
	if err != nil {
		return err
	}
	var central corev1.Container
	for _, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name == "ovn-central" {
			central = container
		}
	}
	for _, target := range record.Targets {
		if target.Container == "recovery" {
			if central.Image == "" {
				return errors.New("recovery helper requires the central image")
			}
			spec := deployment.Spec.Template.Spec
			pod := &corev1.Pod{Name: target.Pod, Namespace: r.client.Namespace, Labels: r.labels(), Spec: corev1.PodSpec{
				NodeName: target.Node, HostNetwork: true, RestartPolicy: corev1.RestartPolicyNever, AutomountServiceAccountToken: new(false),
				SecurityContext: spec.SecurityContext, ImagePullSecrets: spec.ImagePullSecrets, Tolerations: spec.Tolerations,
				Containers: []corev1.Container{{
					Name: "recovery", Image: central.Image, ImagePullPolicy: central.ImagePullPolicy,
					Command: []string{"sleep", "infinity"}, SecurityContext: central.SecurityContext,
					VolumeMounts: []corev1.VolumeMount{{Name: "db", MountPath: "/etc/ovn"}},
				}},
				Volumes: []corev1.Volume{{Name: "db", HostPath: &corev1.HostPathVolumeSource{Path: hostPath, Type: new(corev1.HostPathDirectory)}}},
			}}
			if _, err := r.createPod(ctx, pod); err != nil {
				return err
			}
			if _, err := r.client.waitPod(ctx, pod.Name); err != nil {
				return err
			}
		}
		for _, role := range []string{"nb", "sb"} {
			if _, err := r.client.capture(ctx, target, "test", "-f", "/etc/ovn/ovn"+role+"_db.db"); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Client) recoveryNodes(ctx context.Context, deployment *appsv1.Deployment) ([]string, error) {
	var addresses []string
	for _, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name != "ovn-central" {
			continue
		}
		for _, env := range container.Env {
			if env.Name == "NODE_IPS" {
				if env.ValueFrom != nil {
					return nil, errors.New("recovery requires literal NODE_IPS; indirect membership is unsupported")
				}
				addresses = strings.Split(env.Value, ",")
			}
		}
	}
	if len(addresses) == 0 {
		return nil, errors.New("recovery requires explicit central NODE_IPS to identify every database member")
	}
	nodes, err := c.Kubernetes.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var result []string
	for _, address := range addresses {
		var matches []string
		for _, node := range nodes.Items {
			for _, ip := range node.Status.Addresses {
				if ip.Type == corev1.NodeInternalIP && ip.Address == strings.TrimSpace(address) {
					matches = append(matches, node.Name)
					break
				}
			}
		}
		if len(matches) != 1 {
			return nil, fmt.Errorf("cannot uniquely map central address %q to a node", address)
		}
		if !slices.Contains(result, matches[0]) {
			result = append(result, matches[0])
		}
	}
	if len(result) != int(*deployment.Spec.Replicas) {
		return nil, errors.New("central membership and replica count differ; refusing an ambiguous recovery")
	}
	return result, nil
}

func (c *Client) scaleCentral(ctx context.Context, replicas int32) error {
	scale, err := c.Kubernetes.AppsV1().Deployments(c.Namespace).GetScale(ctx, "ovn-central", metav1.GetOptions{})
	if err != nil {
		return err
	}
	scale.Spec.Replicas = replicas
	_, err = c.Kubernetes.AppsV1().Deployments(c.Namespace).UpdateScale(ctx, "ovn-central", scale, metav1.UpdateOptions{})
	return err
}

func (a *Application) restore(ctx context.Context, client *Client, deployment *appsv1.Deployment, record *recoveryRecord) (resultErr error) {
	run := &resourceRun{client: client, id: record.ID}
	defer func() { resultErr = errors.Join(resultErr, run.cleanup(ctx)) }()
	filename := "kubectl-ko-recovery-" + record.ID + ".json"
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	checkpoint := func(stage string) error {
		record.Stage = stage
		if err := file.Truncate(0); err != nil {
			return err
		}
		if _, err := file.Seek(0, 0); err != nil {
			return err
		}
		if err := json.MarshalWrite(file, record); err != nil {
			return err
		}
		return file.Sync()
	}
	if err := checkpoint("planned"); err != nil {
		return err
	}
	defer func() {
		if resultErr != nil {
			resultErr = fmt.Errorf("recovery stopped at %s; preserve %s and %s on each node; desired replicas were %d: %w", record.Stage, filename, record.Directory, record.Replicas, resultErr)
		}
	}()
	if err := run.prepareRecoveryTargets(ctx, deployment, record); err != nil {
		return err
	}
	if err := checkpoint("stopping-central"); err != nil {
		return err
	}
	if err := client.scaleCentral(ctx, 0); err != nil {
		return err
	}
	if err := client.waitCentralStopped(ctx, deployment); err != nil {
		return err
	}
	if err := checkpoint("backing-up-databases"); err != nil {
		return err
	}
	if err := client.prepareRecoveryFiles(ctx, record); err != nil {
		return err
	}
	if err := checkpoint("replacing-databases"); err != nil {
		return err
	}
	if err := client.replaceRecoveryFiles(ctx, record); err != nil {
		return err
	}
	if err := checkpoint("starting-central"); err != nil {
		return err
	}
	if err := client.scaleCentral(ctx, record.Replicas); err != nil {
		return err
	}
	if err := client.waitDeployment(ctx, "ovn-central", 5*time.Minute); err != nil {
		return err
	}
	for _, role := range []string{"nb", "sb", "northd"} {
		if _, err := client.leader(ctx, role); err != nil {
			return err
		}
	}
	if err := a.databaseStatus(ctx, client, nil); err != nil {
		return err
	}
	if err := checkpoint("restarting-ovs"); err != nil {
		return err
	}
	if err := client.restart(ctx, "daemonset", "ovs-ovn"); err != nil {
		return err
	}
	if err := checkpoint("completed"); err != nil {
		return err
	}
	_, err = fmt.Fprintf(a.streams.Out, "Recovery completed; retained database files in %s and local record %s\n", record.Directory, filename)
	return err
}

func (c *Client) waitCentralStopped(ctx context.Context, deployment *appsv1.Deployment) error {
	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil {
		return err
	}
	if selector.Empty() {
		return errors.New("central selector is empty")
	}
	return wait.PollUntilContextTimeout(ctx, time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
		pods, err := c.Kubernetes.CoreV1().Pods(c.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
		if err != nil {
			return false, err
		}
		return len(pods.Items) == 0, nil
	})
}

func (c *Client) prepareRecoveryFiles(ctx context.Context, record *recoveryRecord) error {
	for _, target := range record.Targets {
		commands := [][]string{
			{"mkdir", "-m", "700", record.Directory},
			{"cp", "-p", "/etc/ovn/ovnnb_db.db", record.Directory + "/ovnnb_db.original"},
			{"cp", "-p", "/etc/ovn/ovnsb_db.db", record.Directory + "/ovnsb_db.original"},
		}
		if target.Node == record.SourceNode {
			commands = append(commands, []string{"ovsdb-tool", "cluster-to-standalone", record.Directory + "/ovnnb_db.standalone", record.Directory + "/ovnnb_db.original"})
		}
		for _, command := range commands {
			if _, err := c.capture(ctx, target, command...); err != nil {
				return err
			}
		}
		if target.Node == record.SourceNode {
			name, err := c.capture(ctx, target, "ovsdb-tool", "db-name", record.Directory+"/ovnnb_db.standalone")
			if err != nil {
				return err
			}
			if strings.TrimSpace(name) != databaseName("nb") {
				return errors.New("recovery source is not OVN_Northbound")
			}
		}
	}
	return nil
}

func (c *Client) replaceRecoveryFiles(ctx context.Context, record *recoveryRecord) error {
	for _, target := range record.Targets {
		for _, role := range []string{"nb", "sb"} {
			if _, err := c.capture(ctx, target, "mv", "/etc/ovn/ovn"+role+"_db.db", record.Directory+"/ovn"+role+"_db.clustered"); err != nil {
				return err
			}
			// A surviving RAFT header makes start-db.sh rejoin the old cluster.
			// Preserve optional headers alongside the old databases before restart.
			header := "/etc/ovn/ovn" + role + "_db.hdr"
			if _, err := c.capture(ctx, target, "test", "-e", header); err != nil {
				if exit, ok := errors.AsType[exec.ExitError](err); ok && exit.ExitStatus() == 1 {
					continue
				}
				return err
			}
			if _, err := c.capture(ctx, target, "mv", header, record.Directory+"/ovn"+role+"_db.hdr"); err != nil {
				return err
			}
		}
	}
	for _, target := range record.Targets {
		if target.Node == record.SourceNode {
			_, err := c.capture(ctx, target, "cp", "-p", record.Directory+"/ovnnb_db.standalone", "/etc/ovn/ovnnb_db.db")
			return err
		}
	}
	return errors.New("recovery source disappeared from the plan")
}
