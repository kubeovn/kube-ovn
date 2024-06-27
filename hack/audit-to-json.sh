#!/bin/bash

# This script is used to convert the audit log to JSON format.

if [ ! -f "$1" ]; then
  echo "Usage: $0 <audit-log-file>"
  exit 1
fi

json_file=$(basename $1 .log).json
cp "$1" "$json_file"
sed -i 's/$/,/g' "$json_file"
sed -i '1 i\[' "$json_file"
sed -i 's/\n\n/\n/g' "$json_file"
sed -i '$ s/,$/\n]/' "$json_file"
