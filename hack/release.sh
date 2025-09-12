#!/bin/bash
set -euo pipefail
# run hack/release.sh from the project root directory to publish the release
DOCS_DIR="$(realpath $(dirname $0)/../../docs)"

DRY_RUN=0
if [ ${1:-} == "--dry-run" ]; then
  DRY_RUN=1
fi

echo "check status of last commit build"
commit=$(git rev-parse HEAD)
# FIXME: get all runs by setting parameter page and per_page if there are more than 100 runs
# Reference: https://docs.github.com/en/rest/checks/runs?apiVersion=2022-11-28#list-check-runs-for-a-git-reference
check_status=$(curl -s https://api.github.com/repos/kubeovn/kube-ovn/commits/$commit/check-runs?per_page=100)
if echo $check_status | grep -q '"status": "queued"'; then
  echo "last commit build is queued"
  exit 1
fi
if echo $check_status | grep -q '"status": "in_progress"'; then
  echo "last commit build is in progress"
  exit 1
fi
if echo $check_status | grep -q '"conclusion": "failure"'; then
  echo "last commit build failed"
  exit 1
fi
if echo $check_status | grep -q '"conclusion": "cancelled"'; then
  echo "last commit build was cancelled"
  exit 1
fi
echo "last commit build successed"


echo "tag and push image"
VERSION=$(cat VERSION)
set +e
docker manifest rm kubeovn/kube-ovn:${VERSION}
docker manifest rm kubeovn/kube-ovn:${VERSION}-debug
docker manifest rm kubeovn/vpc-nat-gateway:${VERSION}
set -e

docker pull kubeovn/kube-ovn:${VERSION}-x86
docker pull kubeovn/kube-ovn:${VERSION}-arm
docker pull kubeovn/vpc-nat-gateway:${VERSION}-x86
docker pull kubeovn/vpc-nat-gateway:${VERSION}-arm
docker pull kubeovn/kube-ovn:${VERSION}-debug-x86
docker pull kubeovn/kube-ovn:${VERSION}-debug-arm

docker manifest create kubeovn/kube-ovn:${VERSION} kubeovn/kube-ovn:${VERSION}-x86 kubeovn/kube-ovn:${VERSION}-arm
docker manifest create kubeovn/vpc-nat-gateway:${VERSION} kubeovn/vpc-nat-gateway:${VERSION}-x86 kubeovn/vpc-nat-gateway:${VERSION}-arm
docker manifest create kubeovn/kube-ovn:${VERSION}-debug kubeovn/kube-ovn:${VERSION}-debug-x86 kubeovn/kube-ovn:${VERSION}-debug-arm

if [ $DRY_RUN -eq 0 ]; then
  docker manifest push kubeovn/kube-ovn:${VERSION}
  docker manifest push kubeovn/vpc-nat-gateway:${VERSION}
  docker manifest push kubeovn/kube-ovn:${VERSION}-debug
fi

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

if [ $DRY_RUN -eq 0 ]; then
  docker manifest push kubeovn/kube-ovn-base:${NEXT_VERSION}
  docker manifest push kubeovn/kube-ovn-base:${NEXT_VERSION}-debug
  docker push kubeovn/kube-ovn-base:${NEXT_VERSION}-amd64-legacy
  docker push kubeovn/kube-ovn-base:${NEXT_VERSION}-dpdk
fi

current_branch=$(git rev-parse --abbrev-ref HEAD)
if [ "$current_branch" != "master" ]; then
  echo "current branch is not master, release a patch version"
  echo "modify tag in install.sh and values.yaml"
  sed -i '/^VERSION=/c\VERSION="'"${VERSION}"'"' dist/images/install.sh
  sed -i 's/tag:\ .*/tag:\ '"${VERSION}"'/' charts/kube-ovn/values.yaml
  sed -i 's/version:\ .*/version:\ '"${VERSION}"'/' charts/kube-ovn/Chart.yaml
  sed -i 's/appVersion:\ .*/appVersion:\ "'"${VERSION#v}"'"/' charts/kube-ovn/Chart.yaml
  if [ -f charts/kube-ovn-v2/values.yaml ]; then
    sed -i 's/tag:\ .*/tag:\ '"${VERSION}"'/' charts/kube-ovn-v2/values.yaml
    sed -i 's/version:\ .*/version:\ '"${VERSION}"'/' charts/kube-ovn-v2/Chart.yaml
    sed -i 's/appVersion:\ .*/appVersion:\ "'"${VERSION#v}"'"/' charts/kube-ovn-v2/Chart.yaml
  fi
  sed -i '/image:/s/v(\d+\.){2}\d+/'"${VERSION}/" yamls/webhook.yaml

  echo "commit, tag and push"
  git add dist/images/install.sh
  git add charts/kube-ovn/values.yaml
  git add charts/kube-ovn/Chart.yaml
  if [ -f charts/kube-ovn-v2/values.yaml ]; then
    git add charts/kube-ovn-v2/values.yaml
    git add charts/kube-ovn-v2/Chart.yaml
  fi
  git commit -m "release ${VERSION}"
  git tag ${VERSION}
  if [ $DRY_RUN -eq 0 ]; then
    git push
    git push origin --tags
  fi

  echo "modify version to next patch number"
  NEXT_VERSION=$(cat VERSION | awk -F '.' '{print $1"."$2"."$3+1}')
  echo ${NEXT_VERSION} > VERSION
  git add VERSION
  git commit -m "prepare for next release"
  if [ $DRY_RUN -eq 0 ]; then
    git push
  fi

  echo "Modify the doc version number"
  cd ${DOCS_DIR}
  git checkout $(echo $VERSION | awk -F '.' '{print $1"."$2}')
  git pull
  sed -i "s/version: .*/version: ${VERSION}/" mkdocs.yml
  git add mkdocs.yml
  git commit -m "update version to ${VERSION}"
  if [ $DRY_RUN -eq 0 ]; then
    git push
  fi

  echo "clean up images"
  docker rmi kubeovn/kube-ovn:${VERSION}-x86 \
    kubeovn/kube-ovn:${VERSION}-arm \
    kubeovn/vpc-nat-gateway:${VERSION}-x86 \
    kubeovn/vpc-nat-gateway:${VERSION}-arm \
    kubeovn/kube-ovn:${VERSION}-debug-x86 \
    kubeovn/kube-ovn:${VERSION}-debug-arm \
    kubeovn/kube-ovn-base:${VERSION}-amd64 \
    kubeovn/kube-ovn-base:${VERSION}-arm64 \
    kubeovn/kube-ovn-base:${VERSION}-amd64-legacy \
    kubeovn/kube-ovn-base:${VERSION}-dpdk \
    kubeovn/kube-ovn-base:${VERSION}-debug-amd64 \
    kubeovn/kube-ovn-base:${VERSION}-debug-arm64

  echo "Manually update the release note with the new changelog"
else
  echo "current branch is master, release a minor version"
  echo "push tag and create new release branch"
  git tag ${VERSION}
  RELEASE_BRANCH=release-$(echo ${VERSION} | sed 's/v\([0-9]*\.[0-9]*\).*/\1/')
  if [ $DRY_RUN -eq 0 ]; then
    git push origin --tags
  fi
  git checkout -b $RELEASE_BRANCH
  if [ $DRY_RUN -eq 0 ]; then
    git push --set-upstream origin $RELEASE_BRANCH
  fi

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
  if [ $DRY_RUN -eq 0 ]; then
    docker manifest push kubeovn/kube-ovn-base:${NEXT_VERSION}
    docker manifest push kubeovn/kube-ovn-base:${NEXT_VERSION}-debug
    docker push kubeovn/kube-ovn-base:${NEXT_VERSION}-amd64-legacy
    docker push kubeovn/kube-ovn-base:${NEXT_VERSION}-dpdk
  fi

  echo "prepare next release in master branch"
  echo ${NEXT_VERSION} > VERSION
  sed -i '/^VERSION=/c\VERSION="'"${NEXT_VERSION}"'"' dist/images/install.sh
  sed -i 's/tag:\ .*/tag:\ '"${NEXT_VERSION}"'/' charts/kube-ovn/values.yaml
  sed -i 's/version:\ .*/version:\ '"${NEXT_VERSION}"'/' charts/kube-ovn/Chart.yaml
  sed -i 's/appVersion:\ .*/appVersion:\ "'"${NEXT_VERSION#v}"'"/' charts/kube-ovn/Chart.yaml
  sed -i 's/tag:\ .*/tag:\ '"${NEXT_VERSION}"'/' charts/kube-ovn-v2/values.yaml
  sed -i 's/version:\ .*/version:\ '"${NEXT_VERSION}"'/' charts/kube-ovn-v2/Chart.yaml
  sed -i 's/appVersion:\ .*/appVersion:\ "'"${NEXT_VERSION#v}"'"/' charts/kube-ovn-v2/Chart.yaml
  sed -ri 's#(\s+)(- master)#\1\2\n\1- '$RELEASE_BRANCH'#' .github/workflows/build-kube-ovn-base.yaml
  sed -ri 's#(\s+)(- master)#\1\2\n\1- '$RELEASE_BRANCH'#' .github/workflows/build-kube-ovn-base-dpdk.yaml
  sed -ri 's#(\s+)(- master)#\1\2\n\1- '$RELEASE_BRANCH'#' .github/workflows/scheduled-e2e.yaml
  sed -i '/image:/s/v(\d+\.){2}\d+/'"${NEXT_VERSION}/" yamls/webhook.yaml

  git add dist/images/install.sh
  git add charts/kube-ovn/values.yaml
  git add charts/kube-ovn/Chart.yaml
  git add charts/kube-ovn-v2/values.yaml
  git add charts/kube-ovn-v2/Chart.yaml
  git add VERSION
  git add .github/workflows/build-kube-ovn-base.yaml
  git add .github/workflows/build-kube-ovn-base-dpdk.yaml
  git add .github/workflows/scheduled-e2e.yaml
  git commit -m "prepare for next release"
  if [ $DRY_RUN -eq 0 ]; then
    git push
  fi

  echo "prepare next release in release branch"
  git checkout $RELEASE_BRANCH
  NEXT_VERSION=$(cat VERSION | awk -F '.' '{print $1"."$2"."$3+1}')
  echo ${NEXT_VERSION} > VERSION
  git add VERSION
  git commit -m "prepare for next release"
  if [ $DRY_RUN -eq 0 ]; then
    git push
  fi

  echo "need manual update the docs to create a new branch"
fi
