SetLabel(){
  echo "$1"
  readarray -t rslts < <(ovn-nbctl --format=csv --data=bare --no-heading --columns=name find "$1")
  for i in "${rslts[@]}"
  do
    ovn-nbctl set "${1}" "${i}" external_ids:vendor=kube-ovn
  done
}

SetLabel logical_switch_port
SetLabel logical_switch
SetLabel logical_router
