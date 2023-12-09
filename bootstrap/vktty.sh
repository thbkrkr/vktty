#!/bin/bash
set -uo pipefail

help() {
  echo 'Usage: vktty.sh COMMAND

  Commands:
    create ID     Create a vcluster applying bootstrap/*.yaml
    delete ID     Delete a vcluster
    get    ID     Get a vcluster
'
}

: $KTTY_TAG

create() {
  export i=$1
  export key=$(uuid_gen)
  vcluster --log-output=json create "c$i" --expose --connect=false 1>&2 \
  && \
  envsubst < bootstrap/*.yaml | vcluster connect c$i -- kubectl apply -f- 1>&2
  echo '{"Status": '$?',"Key":"'$key'"}'
}

delete() {
  i=$1
  vcluster delete --log-output=json "c$1" 1>&2
  echo '{"Status": '$?'}'
}

get() {
  i=$1
  pod=$(kubectl -n vcluster-c$i get pod -o json | jq -c '.items[] | select(.metadata.name | startswith("ktty"))')
  key=$(jq -r '.spec.containers[0].args[2]' <<< "$pod" | cut -d ':' -f2)
  echo '{"Status": '$?', "Key":"'$key'"}'
}

uuid_gen() {
  cat /proc/sys/kernel/random/uuid 2>/dev/null || echo "$RANDOM-$RANDOM-$RANDOM-$RANDOM"
}

main() {
  cmd=$1
  i=$2
  case "$cmd" in
    c|create) create "$i" ;;
    d|delete) delete "$i" ;;
    g|get)    get "$i" ;;
    *)        help ;;
  esac
}

main "$@"
