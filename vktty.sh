#!/bin/bash -eu

main() {
  action=$1
  num=$2
  case "$action" in
    create)
      vcluster create "c$num" --expose
      sed "s/31320/3132$num/" ktty.yaml | kubectl apply -f-
      vcluster disconnect
    ;;
    delete)
      vcluster delete "c$num"
    ;;
  esac
}

main "$@"
