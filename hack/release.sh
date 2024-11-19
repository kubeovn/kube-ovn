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

docker manifest push kubeovn/kube-ovn:${VERSION}
docker manifest push kubeovn/vpc-nat-gateway:${VERSION}
docker manifest push kubeovn/kube-ovn:${VERSION}-debug

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

  echo "trigger action to build new base"
  gh workflow run build-kube-ovn-base.yaml -f branch=$current_branch

  echo "Modify the doc version number"
  cd ${DOCS_DIR}
  git checkout $(cat VERSION | awk -F '.' '{print $1"."$2}')
  git pull
  sed -i "s/version: .*/version: ${VERSION}/" mkdocs.yml
  git add mkdocs.yml
  git commit -m "update version to ${VERSION}"
  git push

  echo "clean up images"
  docker rmi kubeovn/kube-ovn:${VERSION}-x86 kubeovn/kube-ovn:${VERSION}-arm kubeovn/vpc-nat-gateway:${VERSION}-x86 kubeovn/vpc-nat-gateway:${VERSION}-arm kubeovn/kube-ovn:${VERSION}-debug-x86 kubeovn/kube-ovn:${VERSION}-debug-arm

  echo "Manually update the release note with the new changelog"
else
  echo "current branch is master, release a minor version"
  echo "push tag and create new release branch"
  git tag ${VERSION}
  RELEASE_BRANCH=release-$(echo ${VERSION} | sed 's/v\([0-9]*\.[0-9]*\).*/\1/')
  git push origin --tags
  git checkout -b $RELEASE_BRANCH
  git push origin $RELEASE_BRANCH

  echo "prepare next release in master branch"
  git checkout master
  git pull
  NEXT_VERSION=$(cat VERSION | awk -F '.' '{print $1"."$2+1"."$3}') 
  echo ${NEXT_VERSION} > VERSION
  sed -i '/^VERSION=/c\VERSION="'"${NEXT_VERSION}"'"' dist/images/install.sh
  sed -i 's/tag:\ .*/tag:\ '"${NEXT_VERSION}"'/' charts/kube-ovn/values.yaml
  sed -i 's/version:\ .*/version:\ '"${NEXT_VERSION}"'/' charts/kube-ovn/Chart.yaml
  sed -i 's/appVersion:\ .*/appVersion:\ "'"${NEXT_VERSION#v}"'"/' charts/kube-ovn/Chart.yaml

  git add dist/images/install.sh   
  git add charts/kube-ovn/values.yaml
  git add charts/kube-ovn/Chart.yaml
  git add VERSION
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
