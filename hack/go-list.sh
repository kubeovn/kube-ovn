#!/usr/bin/env bash

set -e

path=$1
module=$(grep ^module "$(dirname $0)/../go.mod" | awk '{print $2}')
go list -f '{{ join .Deps "\n" }}' -compiled ./$path/... | grep ^$module/ | while read pkg; do
    d="${pkg#${module}/}" 
    go list -f '{{ join .CompiledGoFiles "\n" }}' -compiled ./$d | while read f; do 
        echo "$d/$f"
    done
done
