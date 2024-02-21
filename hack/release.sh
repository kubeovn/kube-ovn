#!/bin/bash
set -euo pipefail
# run hack/release.sh from the project root directory to publish the release

echo "check status of last commit build"
commit=$(git rev-parse HEAD)
check_status=$(curl https://api.github.com/repos/kubeovn/kube-ovn/commits/$commit/check-runs)
if ! echo $check_status | grep -q '"conclusion": "failure"'; then
    echo "last commit build successed"
else
    echo "last commit build failed"
fi


echo "tag and push image"
VERSION=$(cat VERSION)
docker manifest rm kubeovn/kube-ovn:${VERSION}
docker manifest rm kubeovn/vpc-nat-gateway:${VERSION}

docker pull kubeovn/kube-ovn:${VERSION}-x86
docker pull kubeovn/kube-ovn:${VERSION}-arm
docker pull kubeovn/vpc-nat-gateway:${VERSION}-x86
docker pull kubeovn/vpc-nat-gateway:${VERSION}-arm

docker manifest create kubeovn/kube-ovn:${VERSION} kubeovn/kube-ovn:${VERSION}-x86 kubeovn/kube-ovn:${VERSION}-arm
docker manifest create kubeovn/vpc-nat-gateway:${VERSION} kubeovn/vpc-nat-gateway:${VERSION}-x86 kubeovn/vpc-nat-gateway:${VERSION}-arm

docker manifest push kubeovn/kube-ovn:${VERSION}
docker manifest push kubeovn/vpc-nat-gateway:${VERSION}

echo "modify tag in install.sh and values.yaml"
sed -i '/^VERSION=/c\VERSION="'"${VERSION}"'"' dist/images/install.sh
sed -i 's/tag:\ .*/tag:\ '"${VERSION}"'/' charts/kube-ovn/values.yaml
sed -i 's/version:\ .*/version:\ '"${VERSION}"'/' charts/kube-ovn/Chart.yaml

echo "commit, tag and push"
git add dist/images/install.sh
git add charts/kube-ovn/values.yaml
git add charts/kube-ovn/Chart.yaml
git commit -m "release ${VERSION}"
git tag ${VERSION}
git push
git push origin --tags

echo "modify version to next patch number"
NEXT_VERSION=$(cat VERSION | awk -F '.' '{print $1"."$2"."$3+1}')
echo ${NEXT_VERSION} > VERSION
git add VERSION
git commit -m "prepare for next release"
git push

echo "draft a release"
gh release create $VERSION --draft

echo "trigger action to build new base"
gh workflow run build-kube-ovn-base.yaml --ref $(git branch --show-current)

echo "Need to modify the doc version number manually"