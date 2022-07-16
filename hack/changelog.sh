#!/usr/bin/env sh
set -eu

echo '# Changelog'
echo

tag=
git tag -l 'v*' | sort -rV | while read last; do
  if [ "$tag" != "" ]; then
    echo "## $(git for-each-ref --format='%(refname:strip=2) (%(creatordate:short))' refs/tags/${tag})"
    echo
    git_log='git --no-pager log --no-merges'
	  $git_log --format=' * [%h](https://github.com/kubeovn/kube-ovn/commit/%H) %s' $last..$tag
	  echo
	  echo "### Contributors"
	  echo
	  $git_log --format=' * %an'  $last..$tag | grep -vi oilbeater | sort -u
	  echo
  fi
  tag=$last
done
