#!/bin/bash

if [ "$#" -ne 1 ]; then
   echo "Usage: $0 <availability_zone_uuid> "
   echo "   eg: ./clean-ic-az-db.sh 2106a2d8-f6d4-4645-b05a-59417556eccb "
   exit 1
fi

availability_zone=$1

if ! ovn-ic-sbctl get availability_zone $availability_zone name >/dev/null 2>&1; then
   echo "Availability zone $availability_zone not found."
   exit 1
fi

resource_types=("Gateway" "Route" "Port_Binding")

for resource_type in "${resource_types[@]}"; do
   uuid_array=($(ovn-ic-sbctl --columns=_uuid find $resource_type availability_zone=$availability_zone | awk '{print $3}'))

   for uuid in "${uuid_array[@]}"; do
      ovn-ic-sbctl destroy $resource_type $uuid
      echo "Destroyed $resource_type: $uuid"
   done
done

ovn-ic-sbctl destroy availability_zone $availability_zone
echo "Destroyed availability_zone: $availability_zone"