package ko

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"github.com/kubeovn/kube-ovn/pkg/gcexempt"
)

func gcExemptionTestConfigMap(data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{Name: gcexempt.ConfigMapName, Namespace: "ovn-system", Data: data}
}

func TestGCExemptionAddCreatesConfigMapWhenAbsent(t *testing.T) {
	app, _, _, _ := testApplication(t)

	require.NoError(t, app.Execute(t.Context(), []string{"gc-exemption", "--add-exemption", "--type=lsp", "--name=foo"}))

	client, err := app.newClient()
	require.NoError(t, err)
	cm, err := client.Kubernetes.CoreV1().ConfigMaps("ovn-system").Get(t.Context(), gcexempt.ConfigMapName, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "foo", cm.Data[gcexempt.LSPKey])
}

func TestGCExemptionAddDedupesAndSorts(t *testing.T) {
	app, _, _, _ := testApplication(t, gcExemptionTestConfigMap(map[string]string{gcexempt.ChassisKey: "b,a"}))

	require.NoError(t, app.Execute(t.Context(), []string{"gc-exemption", "--add-exemption", "--type=chassis", "--name=a"}))
	require.NoError(t, app.Execute(t.Context(), []string{"gc-exemption", "--add-exemption", "--type=chassis", "--name=c"}))

	client, err := app.newClient()
	require.NoError(t, err)
	cm, err := client.Kubernetes.CoreV1().ConfigMaps("ovn-system").Get(t.Context(), gcexempt.ConfigMapName, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "a,b,c", cm.Data[gcexempt.ChassisKey])
}

func TestGCExemptionRemoveIsNoOpWhenConfigMapMissing(t *testing.T) {
	app, _, _, _ := testApplication(t)

	require.NoError(t, app.Execute(t.Context(), []string{"gc-exemption", "--remove-exemption", "--type=lsp", "--name=foo"}))

	client, err := app.newClient()
	require.NoError(t, err)
	fakeClient, ok := client.Kubernetes.(*fake.Clientset)
	require.True(t, ok)
	for _, action := range fakeClient.Actions() {
		require.NotEqual(t, "create", action.GetVerb(), "removing from a nonexistent exemption list must not create a configmap")
	}
}

func TestGCExemptionRemoveDeletesNameButKeepsOthers(t *testing.T) {
	app, _, _, _ := testApplication(t, gcExemptionTestConfigMap(map[string]string{gcexempt.LSPKey: "a,b"}))

	require.NoError(t, app.Execute(t.Context(), []string{"gc-exemption", "--remove-exemption", "--type=lsp", "--name=a"}))

	client, err := app.newClient()
	require.NoError(t, err)
	cm, err := client.Kubernetes.CoreV1().ConfigMaps("ovn-system").Get(t.Context(), gcexempt.ConfigMapName, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "b", cm.Data[gcexempt.LSPKey])
}

func TestGCExemptionListPrintsCurrentState(t *testing.T) {
	app, _, out, _ := testApplication(t, gcExemptionTestConfigMap(map[string]string{
		gcexempt.LSPKey:     "a,b",
		gcexempt.ChassisKey: "c",
	}))

	require.NoError(t, app.Execute(t.Context(), []string{"gc-exemption", "--list"}))
	require.Contains(t, out.String(), "a,b")
	require.Contains(t, out.String(), "c")
}

func TestGCExemptionListReportsNoExemptionsWhenMissing(t *testing.T) {
	app, _, out, _ := testApplication(t)

	require.NoError(t, app.Execute(t.Context(), []string{"gc-exemption", "--list"}))
	require.Contains(t, out.String(), "no GC exemptions configured")
}

func TestGCExemptionValidation(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"both add and remove", []string{"gc-exemption", "--add-exemption", "--remove-exemption", "--type=lsp", "--name=foo"}},
		{"neither action", []string{"gc-exemption", "--type=lsp", "--name=foo"}},
		{"missing type", []string{"gc-exemption", "--add-exemption", "--name=foo"}},
		{"invalid type", []string{"gc-exemption", "--add-exemption", "--type=bogus", "--name=foo"}},
		{"empty name", []string{"gc-exemption", "--add-exemption", "--type=lsp", "--name= "}},
		{"name with comma", []string{"gc-exemption", "--add-exemption", "--type=lsp", "--name=a,b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, _, _, _ := testApplication(t)
			err := app.Execute(t.Context(), tt.args)
			require.Error(t, err)
			require.Equal(t, 2, ExitCode(err))
		})
	}
}

func TestGCExemptionAddRetriesOnConflict(t *testing.T) {
	app, _, _, _ := testApplication(t, gcExemptionTestConfigMap(map[string]string{gcexempt.LSPKey: "a"}))

	client, err := app.newClient()
	require.NoError(t, err)
	fakeClient, ok := client.Kubernetes.(*fake.Clientset)
	require.True(t, ok)

	conflictOnce := true
	fakeClient.PrependReactor("update", "configmaps", func(_ ktesting.Action) (bool, runtime.Object, error) {
		if conflictOnce {
			conflictOnce = false
			return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, gcexempt.ConfigMapName, errors.New("stale"))
		}
		return false, nil, nil
	})

	require.NoError(t, app.Execute(t.Context(), []string{"gc-exemption", "--add-exemption", "--type=lsp", "--name=b"}))

	cm, err := fakeClient.CoreV1().ConfigMaps("ovn-system").Get(t.Context(), gcexempt.ConfigMapName, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "a,b", cm.Data[gcexempt.LSPKey])
}
