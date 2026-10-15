package ko

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"

	"github.com/kubeovn/kube-ovn/pkg/gcexempt"
)

type gcExemptionOptions struct {
	addChassis, removeChassis, listChassis bool
	chassisName                            string
}

func (a *Application) addGCCommands() {
	options := gcExemptionOptions{}
	cmd := &cobra.Command{
		Use:   "gc-exemption (--add-chassis-exemption | --remove-chassis-exemption | --list-chassis-exemptions) --chassis-name NAME",
		Short: "Add, remove, or list chassis names exempted from Kube-OVN's chassis garbage collection",
		Args: func(_ *cobra.Command, _ []string) error {
			set := 0
			for _, v := range []bool{options.addChassis, options.removeChassis, options.listChassis} {
				if v {
					set++
				}
			}
			if set != 1 {
				return errors.New("specify exactly one of --add-chassis-exemption, --remove-chassis-exemption, or --list-chassis-exemptions")
			}
			if options.listChassis {
				return nil
			}
			name := strings.TrimSpace(options.chassisName)
			if name == "" {
				return errors.New("--chassis-name must not be empty")
			}
			if strings.Contains(name, ",") {
				return errors.New("--chassis-name must not contain a comma")
			}
			return nil
		},
		RunE: a.run(func(ctx context.Context, client *Client, _ []string) error {
			if options.listChassis {
				return client.listChassisGCExemptions(ctx, a.streams.Out)
			}
			return client.updateChassisGCExemption(ctx, strings.TrimSpace(options.chassisName), options.addChassis)
		}),
	}
	// Shorthands avoid -h/-n/-s, which genericclioptions.ConfigFlags already
	// binds globally for --help/--namespace/--server.
	cmd.Flags().BoolVarP(&options.addChassis, "add-chassis-exemption", "a", false, "Add --chassis-name to the chassis GC exemption list")
	cmd.Flags().BoolVarP(&options.removeChassis, "remove-chassis-exemption", "r", false, "Remove --chassis-name from the chassis GC exemption list")
	cmd.Flags().BoolVarP(&options.listChassis, "list-chassis-exemptions", "l", false, "Print the current chassis GC exemption list")
	cmd.Flags().StringVarP(&options.chassisName, "chassis-name", "c", "", "The chassis name to add or remove")
	a.root.AddCommand(cmd)
}

// updateChassisGCExemption adds or removes name from the comma-separated
// chassis exemption list stored in the ovn-gc-exemption-config ConfigMap,
// creating the ConfigMap on first use. Removing from a list that doesn't
// exist yet is a no-op. Retried on conflict so concurrent admin edits don't
// clobber each other, matching the Get/DeepCopy/mutate/Update-under-retry
// idiom used by updateKubeOVNTLSSecretData in pkg/controller/kube_ovn_tls.go.
func (c *Client) updateChassisGCExemption(ctx context.Context, name string, add bool) error {
	cms := c.Kubernetes.CoreV1().ConfigMaps(c.Namespace)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cm, err := cms.Get(ctx, gcexempt.ConfigMapName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			if !add {
				return nil
			}
			cm = &corev1.ConfigMap{
				Name: gcexempt.ConfigMapName, Namespace: c.Namespace,
				Data: map[string]string{gcexempt.ChassisKey: name},
			}
			_, err = cms.Create(ctx, cm, metav1.CreateOptions{})
			return err
		}
		if err != nil {
			return err
		}
		cm = cm.DeepCopy()
		if cm.Data == nil {
			cm.Data = map[string]string{}
		}
		names := splitGCExemptionNames(cm.Data[gcexempt.ChassisKey])
		if add {
			names = addGCExemptionName(names, name)
		} else {
			names = removeGCExemptionName(names, name)
		}
		cm.Data[gcexempt.ChassisKey] = strings.Join(names, ",")
		_, err = cms.Update(ctx, cm, metav1.UpdateOptions{})
		return err
	})
}

func (c *Client) listChassisGCExemptions(ctx context.Context, out io.Writer) error {
	cm, err := c.Kubernetes.CoreV1().ConfigMaps(c.Namespace).Get(ctx, gcexempt.ConfigMapName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err := fmt.Fprintln(out, "no chassis GC exemptions configured")
		return err
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "chassis: %s\n", cm.Data[gcexempt.ChassisKey])
	return err
}

func splitGCExemptionNames(value string) []string {
	seen := make(map[string]struct{})
	var names []string
	for name := range strings.SplitSeq(value, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func addGCExemptionName(names []string, name string) []string {
	if slices.Contains(names, name) {
		return names
	}
	names = append(names, name)
	sort.Strings(names)
	return names
}

func removeGCExemptionName(names []string, name string) []string {
	out := names[:0:0]
	for _, n := range names {
		if n != name {
			out = append(out, n)
		}
	}
	return out
}
