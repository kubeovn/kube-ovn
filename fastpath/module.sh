#!/bin/bash
set -euo pipefail

rm -f /tmp/kube_ovn_fastpath.ko

centosins(){
  sys="$1"; shift
  way="$1"; shift
  case $sys in
  centos)
    case $way in
    install)
      yum install -y kernel-devel-$(uname -r)
      make all
      cp ./kube_ovn_fastpath.ko /tmp/kube_ovn_fastpath.ko
      ;;
    local-install)
      # shellcheck disable=SC2145
      rpm -i /tmp/"$@"
      make all
      cp ./kube_ovn_fastpath.ko /tmp/kube_ovn_fastpath.ko
      ;;
    *)
      echo "unknown action $way"
    esac
    ;;
  *)
    echo "unknown system $sys "
  esac
}

sttins(){
  sys="$1"; shift
    way="$1"; shift
    case $sys in
    stt)
      case $way in
      install)
        yum install -y kernel-devel-$(uname -r)
        cd /ovs/
        sed -i 's/INT_MAX/NF_IP_PRI_FIRST/g' ./datapath/linux/compat/stt.c
        ./boot.sh
        ./configure --with-linux=/lib/modules/$(uname -r)/build CFLAGS="-g -O2 -mpopcnt -msse4.2"
        make rpm-fedora-kmod
        cp ./rpm/rpmbuild/RPMS/x86_64/* /tmp/
        ;;
      local-install)
        # shellcheck disable=SC2145
        rpm -i /tmp/"$@"
        cd /ovs/
        sed -i 's/INT_MAX/NF_IP_PRI_FIRST/g' ./datapath/linux/compat/stt.c
        ./boot.sh
        ./configure --with-linux=/lib/modules/$(uname -r)/build CFLAGS="-g -O2 -mpopcnt -msse4.2"
        make rpm-fedora-kmod
        cp ./rpm/rpmbuild/RPMS/x86_64/* /tmp/
        ;;
      *)
        echo "unknown action $way"
      esac
      ;;
    *)
      echo "unknown command $sys "
    esac
}

subcommand="$1"; shift
case $subcommand in
  centos)
    centosins "$subcommand" "$@"
    ;;
  stt)
    sttins "$subcommand" "$@"
    ;;
  *)
    echo "unknown subcommand $subcommand"
    ;;
esac

