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

function create() {
  export i=$1
  export uuid=$(uuid_gen)
  
  vcluster --log-output=json create "c$i" --expose --connect=false 1>&2 \
  && \
  envsubst < bootstrap/ktty.yaml | vcluster connect c$i -- kubectl apply -f- 1>&2

  echo '{"Status": '$?',"Key":"'$uuid'"}'
}

delete() {
  i=$1
  vcluster delete --log-output=json "c$1" 1>&2
  echo '{"Status": '$?'}'
}

uuid_gen() {
  cat /proc/sys/kernel/random/uuid 2>/dev/null || echo "$RANDOM-$RANDOM-$RANDOM-$RANDOM"
}

main() {
  action=$1
  i=$2
  bootstrap_file=${3:-}
  case "$action" in
    c|create) create "$i" ;;
    d|delete) delete "$i" ;;
    *)      help ;;
  esac
}

main "$@"
