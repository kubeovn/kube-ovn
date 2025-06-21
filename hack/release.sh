#!/bin/bash
set -euo pipefail
# run hack/release.sh from the project root directory to publish the release
DOCS_DIR="../docs"

echo "check status of last commit build"
commit=$(git rev-parse HEAD)
check_status=$(curl https://api.github.com/repos/kubeovn/kube-ovn/commits/$commit/check-runs)
if ! echo $check_status | grep -q '"conclusion": "failure"'; then
    echo "last commit build successed"
else
    echo "last commit build failed"
    exit 1
fi


echo "tag and push image"
VERSION=$(cat VERSION)
set +e
docker manifest rm kubeovn/kube-ovn:${VERSION}
docker manifest rm kubeovn/kube-ovn:${VERSION}-debug
docker manifest rm kubeovn/vpc-nat-gateway:${VERSION}
set -e

docker pull kubeovn/kube-ovn:${VERSION}-amd64
docker pull kubeovn/kube-ovn:${VERSION}-arm64
docker pull kubeovn/vpc-nat-gateway:${VERSION}-amd64
docker pull kubeovn/vpc-nat-gateway:${VERSION}-arm64
docker pull kubeovn/kube-ovn:${VERSION}-debug-amd64
docker pull kubeovn/kube-ovn:${VERSION}-debug-arm64

docker manifest create kubeovn/kube-ovn:${VERSION} kubeovn/kube-ovn:${VERSION}-amd64 kubeovn/kube-ovn:${VERSION}-arm64
docker manifest create kubeovn/vpc-nat-gateway:${VERSION} kubeovn/vpc-nat-gateway:${VERSION}-amd64 kubeovn/vpc-nat-gateway:${VERSION}-arm64
docker manifest create kubeovn/kube-ovn:${VERSION}-debug kubeovn/kube-ovn:${VERSION}-debug-amd64 kubeovn/kube-ovn:${VERSION}-debug-arm64

docker manifest push kubeovn/kube-ovn:${VERSION}
docker manifest push kubeovn/vpc-nat-gateway:${VERSION}
docker manifest push kubeovn/kube-ovn:${VERSION}-debug

NEXT_VERSION=$(cat VERSION | awk -F '.' '{print $1"."$2"."$3+1}')
echo "create and push base images for the next version ${NEXT_VERSION}"
set +e
docker manifest rm kubeovn/kube-ovn-base:${NEXT_VERSION}
docker manifest rm kubeovn/kube-ovn-base:${NEXT_VERSION}-debug
set -e
docker pull kubeovn/kube-ovn-base:${VERSION}-amd64
docker pull kubeovn/kube-ovn-base:${VERSION}-arm64
docker pull kubeovn/kube-ovn-base:${VERSION}-amd64-legacy
docker pull kubeovn/kube-ovn-base:${VERSION}-dpdk
docker pull kubeovn/kube-ovn-base:${VERSION}-debug-amd64
docker pull kubeovn/kube-ovn-base:${VERSION}-debug-arm64
docker manifest create kubeovn/kube-ovn-base:${NEXT_VERSION} kubeovn/kube-ovn-base:${VERSION}-amd64 kubeovn/kube-ovn-base:${VERSION}-arm64
docker manifest create kubeovn/kube-ovn-base:${NEXT_VERSION}-debug kubeovn/kube-ovn-base:${VERSION}-debug-amd64 kubeovn/kube-ovn-base:${VERSION}-debug-arm64
docker tag kubeovn/kube-ovn-base:${VERSION}-amd64-legacy kubeovn/kube-ovn-base:${NEXT_VERSION}-amd64-legacy
docker tag kubeovn/kube-ovn-base:${VERSION}-dpdk kubeovn/kube-ovn-base:${NEXT_VERSION}-dpdk
docker manifest push kubeovn/kube-ovn-base:${NEXT_VERSION}
docker manifest push kubeovn/kube-ovn-base:${NEXT_VERSION}-debug
docker push kubeovn/kube-ovn-base:${NEXT_VERSION}-amd64-legacy
docker push kubeovn/kube-ovn-base:${NEXT_VERSION}-dpdk

current_branch=$(git rev-parse --abbrev-ref HEAD)
if [ "$current_branch" != "master" ]; then
  echo "current branch is not master, release a patch version"
  echo "modify tag in install.sh and values.yaml"
  sed -i '/^VERSION=/c\VERSION="'"${VERSION}"'"' dist/images/install.sh
  sed -i 's/tag:\ .*/tag:\ '"${VERSION}"'/' charts/kube-ovn/values.yaml
  sed -i 's/version:\ .*/version:\ '"${VERSION}"'/' charts/kube-ovn/Chart.yaml
  sed -i 's/appVersion:\ .*/appVersion:\ "'"${VERSION#v}"'"/' charts/kube-ovn/Chart.yaml

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

  echo "Modify the doc version number"
  cd ${DOCS_DIR}
  git checkout $(cat VERSION | awk -F '.' '{print $1"."$2}')
  git pull
  sed -i "s/version: .*/version: ${VERSION}/" mkdocs.yml
  git add mkdocs.yml
  git commit -m "update version to ${VERSION}"
  git push

  echo "clean up images"
  docker rmi kubeovn/kube-ovn:${VERSION}-amd64 kubeovn/kube-ovn:${VERSION}-arm64 kubeovn/vpc-nat-gateway:${VERSION}-amd64 kubeovn/vpc-nat-gateway:${VERSION}-arm64 kubeovn/kube-ovn:${VERSION}-debug-amd64 kubeovn/kube-ovn:${VERSION}-debug-arm64

  echo "Manually update the release note with the new changelog"
else
  echo "current branch is master, release a minor version"
  echo "push tag and create new release branch"
  git tag ${VERSION}
  RELEASE_BRANCH=release-$(echo ${VERSION} | sed 's/v\([0-9]*\.[0-9]*\).*/\1/')
  git push origin --tags
  git checkout -b $RELEASE_BRANCH
  git push origin $RELEASE_BRANCH

  echo "create and push base images for the master branch"
  git checkout master
  git pull
  NEXT_VERSION=$(cat VERSION | awk -F '.' '{print $1"."$2+1"."$3}')
  set +e
  docker manifest rm kubeovn/kube-ovn-base:${NEXT_VERSION}
  docker manifest rm kubeovn/kube-ovn-base:${NEXT_VERSION}-debug
  set -e
  docker manifest create kubeovn/kube-ovn-base:${NEXT_VERSION} kubeovn/kube-ovn-base:${VERSION}-amd64 kubeovn/kube-ovn-base:${VERSION}-arm64
  docker manifest create kubeovn/kube-ovn-base:${NEXT_VERSION}-debug kubeovn/kube-ovn-base:${VERSION}-debug-amd64 kubeovn/kube-ovn-base:${VERSION}-debug-arm64
  docker tag kubeovn/kube-ovn-base:${VERSION}-amd64-legacy kubeovn/kube-ovn-base:${NEXT_VERSION}-amd64-legacy
  docker tag kubeovn/kube-ovn-base:${VERSION}-dpdk kubeovn/kube-ovn-base:${NEXT_VERSION}-dpdk
  docker manifest push kubeovn/kube-ovn-base:${NEXT_VERSION}
  docker manifest push kubeovn/kube-ovn-base:${NEXT_VERSION}-debug
  docker push kubeovn/kube-ovn-base:${NEXT_VERSION}-amd64-legacy
  docker push kubeovn/kube-ovn-base:${NEXT_VERSION}-dpdk

  echo "prepare next release in master branch"
  echo ${NEXT_VERSION} > VERSION
  sed -i '/^VERSION=/c\VERSION="'"${NEXT_VERSION}"'"' dist/images/install.sh
  sed -i 's/tag:\ .*/tag:\ '"${NEXT_VERSION}"'/' charts/kube-ovn/values.yaml
  sed -i 's/version:\ .*/version:\ '"${NEXT_VERSION}"'/' charts/kube-ovn/Chart.yaml
  sed -i 's/appVersion:\ .*/appVersion:\ "'"${NEXT_VERSION#v}"'"/' charts/kube-ovn/Chart.yaml
  sed -ri 's#(\s+)(- master)#\1\2\n\1- '$RELEASE_BRANCH'#' .github/workflows/build-kube-ovn-base.yaml
  sed -ri 's#(\s+)(- master)#\1\2\n\1- '$RELEASE_BRANCH'#' .github/workflows/build-kube-ovn-base-dpdk.yaml
  sed -ri 's#(\s+)(- master)#\1\2\n\1- '$RELEASE_BRANCH'#' .github/workflows/scheduled-e2e.yaml

  git add dist/images/install.sh
  git add charts/kube-ovn/values.yaml
  git add charts/kube-ovn/Chart.yaml
  git add VERSION
  git add .github/workflows/build-kube-ovn-base.yaml
  git add .github/workflows/build-kube-ovn-base-dpdk.yaml
  git add .github/workflows/scheduled-e2e.yaml
  git commit -m "prepare for next release"
  git push

  echo "prepare next release in release branch"
  git checkout $RELEASE_BRANCH
  NEXT_VERSION=$(cat VERSION | awk -F '.' '{print $1"."$2"."$3+1}')
  echo ${NEXT_VERSION} > VERSION
  git add VERSION
  git commit -m "prepare for next release"
  git push

  echo "need manual update the docs to create a new branch"
fi
