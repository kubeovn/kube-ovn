#!/bin/bash

set -e

GO=${GO:-go}

export CGO_ENABLED=${CGO_ENABLED:-0}

TRIVY_DIR=/trivy
DEPS_DIR=/godeps

GO_INSTALL="$GO install -v -trimpath"

export GOBIN="$DEPS_DIR"

mkdir -p "$DEPS_DIR"

for t in $(cat "$TRIVY_DIR/trivy-targets.txt"); do
    echo "Building $t from source..."
    name=${t%@*}
    version=${t#*@}
    case $name in
        loopback|macvlan)
            build_flags="-ldflags '-extldflags -static -X github.com/containernetworking/plugins/pkg/utils/buildversion.BuildVersion=$version'"
            eval $GO_INSTALL $build_flags github.com/containernetworking/plugins/plugins/main/$name@$version
            ;;
        portmap)
            build_flags="-ldflags '-extldflags -static -X github.com/containernetworking/plugins/pkg/utils/buildversion.BuildVersion=$version'"
            eval $GO_INSTALL $build_flags github.com/containernetworking/plugins/plugins/meta/$name@$version
            ;;
        kubectl)
            mkdir k8s-$version
            curl -sSf -L --retry 5 https://github.com/kubernetes/kubernetes/archive/refs/tags/$version.tar.gz | \
                tar -xz --strip-components=1 -C k8s-$version
            cd k8s-$version
            source hack/lib/util.sh
            source hack/lib/logging.sh
            source hack/lib/version.sh
            repo=kubernetes/kubernetes
            commit=unknown
            read type tag_sha < <(echo $(curl -s "https://api.github.com/repos/$repo/git/ref/tags/$version" | jq -r '.object.type,.object.sha'))
            if [ $type = "commit" ]; then
                commit=$tag_sha
            else
                commit=$(curl -s "https://api.github.com/repos/$repo/git/tags/$tag_sha" | jq -r '.object.sha')
            fi
            export KUBE_GIT_COMMIT="${commit}"
            export KUBE_GIT_TREE_STATE='clean'
            export KUBE_GIT_VERSION="${version}"
            export KUBE_GIT_MAJOR=$(echo $KUBE_GIT_VERSION | cut -d. -f1 | sed 's/$v//')
            export KUBE_GIT_MINOR=$(echo $KUBE_GIT_VERSION | cut -d. -f2)
            goldflags="all=$(kube::version::ldflags) -s -w"
            $GO_INSTALL -ldflags="${goldflags}" k8s.io/kubernetes/cmd/kubectl
            cd -
            ;;
        gobgp)
            $GO_INSTALL github.com/osrg/gobgp/v4/cmd/$name@$version
            ;;
        *)
            echo "Unknown go binary: $f"
            exit 1
            ;;
    esac
done

for f in $(ls "$TRIVY_DIR"); do
    f=$(basename $f)
    if [ -x "$TRIVY_DIR/$f" -a ! -e "$DEPS_DIR/$f" ]; then
        cp "$TRIVY_DIR/$f" "$DEPS_DIR"
    fi
done

ls -lh "$DEPS_DIR"
