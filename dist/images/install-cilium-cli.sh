#!/usr/bin/env bash

set -e

CILIUM_CLI_VERSION=$(curl -s https://raw.githubusercontent.com/cilium/cilium-cli/master/stable.txt)
CILIUM_CLI_ARCH=amd64
if [ "$(uname -m)" = "aarch64" ]; then
 CILIUM_CLI_ARCH=arm64
fi

curl -L --fail --remote-name-all https://github.com/cilium/cilium-cli/releases/download/${CILIUM_CLI_VERSION}/cilium-linux-${CILIUM_CLI_ARCH}.tar.gz{,.sha256sum}
if command -v sha256sum >/dev/null; then
  sha256sum --check cilium-linux-${CILIUM_CLI_ARCH}.tar.gz.sha256sum
fi
sudo tar xzvfC cilium-linux-${CILIUM_CLI_ARCH}.tar.gz /usr/local/bin
rm cilium-linux-${CILIUM_CLI_ARCH}.tar.gz{,.sha256sum}
