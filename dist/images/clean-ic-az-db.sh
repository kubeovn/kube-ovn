#!/bin/bash

if [ "$#" -ne 2 ]; then
   echo "use method $0 {az|node} {azName|nodeName}"
   echo "   eg: ./clean-ic-az-db.sh az az1"
   echo "   eg: ./clean-ic-az-db.sh node kube-ovn-worker; it will delete all resource of az that the node belong to"
   exit 1
fi

filter_type=$1
filter_value=$2
availability_zone_uuid=

if [ "$filter_type" != "az" ] && [ "$filter_type" != "node" ]; then
  echo "filter_type should be az or node."
  exit 1
fi

if [ "$filter_type" == "az" ]; then
  availability_zone_uuid=$(ovn-ic-sbctl --columns=_uuid find availability_zone name=$filter_value | awk '{print $3}')
fi

echo $availability_zone_uuid

if [ "$filter_type" == "node" ]; then
  availability_zone_uuid=$(ovn-ic-sbctl --columns=availability_zone find gateway hostname=$filter_value | awk '{print $3}')
fi

if ! ovn-ic-sbctl get availability_zone $availability_zone_uuid name >/dev/null 2>&1; then
   echo "Availability zone $availability_zone_uuid not found."
   exit 1
fi

resource_types=("Gateway" "Route" "Port_Binding")

for resource_type in "${resource_types[@]}"; do
   uuid_array=($(ovn-ic-sbctl --columns=_uuid find $resource_type availability_zone=$availability_zone_uuid | awk '{print $3}'))

   for uuid in "${uuid_array[@]}"; do
      ovn-ic-sbctl destroy $resource_type $uuid
      echo "Destroyed $resource_type: $uuid"
   done
done

ovn-ic-sbctl destroy availability_zone $availability_zone_uuid
echo "Destroyed availability_zone: $availability_zone_uuid"

