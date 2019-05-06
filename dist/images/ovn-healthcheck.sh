#!/bin/bash
set -euo pipefail

ovn-nbctl show
# wait 5 seconds
ovn-sbctl -t 5 show
