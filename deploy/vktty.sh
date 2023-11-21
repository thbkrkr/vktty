#!/bin/bash
set -uo pipefail

help() {
  echo 'Usage: vktty.sh COMMAND

  Commands:
    create ID     Create a vcluster with ktty
    delete ID     Delete a vcluster
'
}

: $KTTY_TAG

boostrap_template=deploy/bootstrap-ktty.yaml

uuid_gen() {
  cat /proc/sys/kernel/random/uuid 2>/dev/null || echo "$RANDOM-$RANDOM-$RANDOM-$RANDOM"
}

boostrap_template() {
  uuid="$1"
  tag="$KTTY_TAG"
  sed -e "s/latest/$tag/" -e "s/31320/3132$i/" -e "s/yolo/$uuid/" "$boostrap_template"
}

create() {
  i=$1

  vcluster create "c$i" --expose --connect=false \
    1>&2 | tee log/c$i-vcluster-create.log

  status=$?
  mv log/c$i-vcluster-create.log log/c$i-vcluster-create-$status.log

  uuid=$(uuid_gen)
  boostrap_template "$uuid" | vcluster connect c$i -- kubectl apply -f- 1>&2 | tee log/c$i-kubectl-apply.log

  status2=$?
  mv log/c$i-kubectl-apply.log log/c$i-kubectl-apply-$status2.log

  echo '{"Password":"'$uuid'", "Status": '$(($status+$status2))', "Log": "'$(cat log/c$i* | base64)'"}'
}

delete() {
  i=$1

  vcluster delete "c$i" 1>&2 | tee log/c$i-vcluster-delete.log

  status=$?
  mv log/c$i-vcluster-delete.log log/c$i-vcluster-delete-$status.log

  echo '{"Status": '$status', "Log": "'$(cat log/c$i* | base64)'"}'
}

main() {
  action=$1
  i=$2
  
  mkdir -p log
  rm -rf log/c$i-*
  case "$action" in
    c|create) create "$i" ;;
    d|delete) delete "$i" ;;
    *)      help ;;
  esac
}

main "$@"
