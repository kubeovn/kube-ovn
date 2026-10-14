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
	add, remove, list bool
	exemptionType     string
	name              string
}

func (o *gcExemptionOptions) key() (string, error) {
	switch o.exemptionType {
	case "lsp":
		return gcexempt.LSPKey, nil
	case "chassis":
		return gcexempt.ChassisKey, nil
	default:
		return "", errors.New(`--type must be "lsp" or "chassis"`)
	}
}

func (a *Application) addGCCommands() {
	options := gcExemptionOptions{}
	cmd := &cobra.Command{
		Use:   "gc-exemption (--add-exemption | --remove-exemption | --list) --type (lsp|chassis) --name NAME",
		Short: "Add, remove, or list names exempted from Kube-OVN's LSP and chassis garbage collection",
		Args: func(_ *cobra.Command, _ []string) error {
			set := 0
			for _, v := range []bool{options.add, options.remove, options.list} {
				if v {
					set++
				}
			}
			if set != 1 {
				return errors.New("specify exactly one of --add-exemption, --remove-exemption, or --list")
			}
			if options.list {
				return nil
			}
			if _, err := options.key(); err != nil {
				return err
			}
			name := strings.TrimSpace(options.name)
			if name == "" {
				return errors.New("--name must not be empty")
			}
			if strings.Contains(name, ",") {
				return errors.New("--name must not contain a comma")
			}
			return nil
		},
		RunE: a.run(func(ctx context.Context, client *Client, _ []string) error {
			if options.list {
				return client.listGCExemptions(ctx, a.streams.Out)
			}
			key, err := options.key()
			if err != nil {
				return err
			}
			return client.updateGCExemption(ctx, key, strings.TrimSpace(options.name), options.add)
		}),
	}
	cmd.Flags().BoolVar(&options.add, "add-exemption", false, "Add --name to the GC exemption list selected by --type")
	cmd.Flags().BoolVar(&options.remove, "remove-exemption", false, "Remove --name from the GC exemption list selected by --type")
	cmd.Flags().BoolVar(&options.list, "list", false, "Print the current GC exemption lists")
	cmd.Flags().StringVar(&options.exemptionType, "type", "", `The exemption list to modify: "lsp" or "chassis"`)
	cmd.Flags().StringVar(&options.name, "name", "", "The logical switch port or chassis name to add or remove")
	a.root.AddCommand(cmd)
}

// updateGCExemption adds or removes name from the comma-separated list
// stored under key in the ovn-gc-exemption-config ConfigMap, creating the
// ConfigMap on first use. Removing from a list that doesn't exist yet is a
// no-op. Retried on conflict so concurrent admin edits don't clobber each
// other, matching the Get/DeepCopy/mutate/Update-under-retry idiom used by
// updateKubeOVNTLSSecretData in pkg/controller/kube_ovn_tls.go.
func (c *Client) updateGCExemption(ctx context.Context, key, name string, add bool) error {
	cms := c.Kubernetes.CoreV1().ConfigMaps(c.Namespace)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cm, err := cms.Get(ctx, gcexempt.ConfigMapName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			if !add {
				return nil
			}
			cm = &corev1.ConfigMap{
				Name: gcexempt.ConfigMapName, Namespace: c.Namespace,
				Data: map[string]string{key: name},
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
		names := splitGCExemptionNames(cm.Data[key])
		if add {
			names = addGCExemptionName(names, name)
		} else {
			names = removeGCExemptionName(names, name)
		}
		cm.Data[key] = strings.Join(names, ",")
		_, err = cms.Update(ctx, cm, metav1.UpdateOptions{})
		return err
	})
}

func (c *Client) listGCExemptions(ctx context.Context, out io.Writer) error {
	cm, err := c.Kubernetes.CoreV1().ConfigMaps(c.Namespace).Get(ctx, gcexempt.ConfigMapName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err := fmt.Fprintln(out, "no GC exemptions configured")
		return err
	}
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "lsp: %s\n", cm.Data[gcexempt.LSPKey]); err != nil {
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
