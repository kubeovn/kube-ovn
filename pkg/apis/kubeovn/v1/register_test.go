package v1

import (
	"go/types"
	"path"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/set"

	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/apis/kubeovn"
)

func TestKind(t *testing.T) {
	gk := Kind("foo")
	require.Equal(t, schema.GroupKind{Group: kubeovn.GroupName, Kind: "foo"}, gk)
}

func TestResource(t *testing.T) {
	gr := Resource("foo")
	require.Equal(t, schema.GroupResource{Group: kubeovn.GroupName, Resource: "foo"}, gr)
}

func getPackagePath(t *testing.T) string {
	t.Helper()

	pc, _, _, _ := runtime.Caller(1)
	funcName := runtime.FuncForPC(pc).Name()
	base := path.Base(funcName)
	index := strings.LastIndexByte(base, '.')
	require.Greater(t, index, 0)

	return path.Join(path.Dir(funcName), base[:index])
}

func TestAddResources(t *testing.T) {
	pkgPath := getPackagePath(t)

	config := &packages.Config{
		Mode: packages.NeedTypes | packages.NeedTypesInfo,
	}
	pkgs, err := packages.Load(config, pkgPath)
	require.NoError(t, err, "failed to load package %q", pkgPath)
	require.Len(t, pkgs, 1, "expected exactly one package, got %d", len(pkgs))

	scheme := k8sruntime.NewScheme()
	err = addKnownTypes(scheme)
	require.NoError(t, err)

	addedTypes := set.New[string]()
	for name, vt := range scheme.KnownTypes(SchemeGroupVersion) {
		if vt.PkgPath() != pkgPath {
			continue
		}
		require.Implements(t, (*k8sruntime.Object)(nil), reflect.New(vt).Interface())
		addedTypes.Insert(name)
	}

	scope := pkgs[0].Types.Scope()
	require.NotNil(t, scope)
	typeMetaType := reflect.TypeFor[metav1.TypeMeta]()
	objMetaType := reflect.TypeFor[metav1.ObjectMeta]()
	listMetaType := reflect.TypeFor[metav1.ListMeta]()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		if !obj.Exported() {
			continue
		}

		st, ok := obj.Type().Underlying().(*types.Struct)
		if !ok {
			continue
		}

		var hasTypeMeta, hasObjMeta, hasListMeta bool
		for v := range st.Fields() {
			if !v.Embedded() || !v.Exported() {
				continue
			}
			switch v.Name() {
			case typeMetaType.Name():
				hasTypeMeta = true
			case objMetaType.Name():
				hasObjMeta = true
			case listMetaType.Name():
				hasListMeta = true
			}
		}
		if hasTypeMeta && (hasObjMeta || hasListMeta) {
			require.True(t, addedTypes.Has(name), "type %q not registered", name)
		}
	}
}
