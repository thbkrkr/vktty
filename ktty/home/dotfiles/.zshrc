#
# ZSH configuration propulsed by https://github.com/robbyrussell/oh-my-zsh
# and https://github.com/thbkrkr/dotfiles
#

ZSH=$HOME/.oh-my-zsh
ZSH_THEME="pure-thb"

#CASE_SENSITIVE="true"
DISABLE_AUTO_UPDATE="true"
DISABLE_UPDATE_PROMPT="true"
COMPLETION_WAITING_DOTS="true"

plugins=(git history-substring-search kubectl helm)
source $ZSH/oh-my-zsh.sh

export HISTSIZE=10000000
export PATH=~/bin:/usr/games:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

# Show completion menu when number of options is at least 2
zstyle ':completion:*' menu select=2

#source <(kubectl completion zsh)

#. /opt/homebrew/etc/profile.d/z.sh

[ -f ~/.fzf.zsh ] && source ~/.fzf.zsh

export PATH="${KREW_ROOT:-$HOME/.krew}/bin:$PATH"

##########################

# Tiny aliases
alias a='ansible'
alias ap='ansible-playbook'
alias c='clear'
alias d='docker'
alias dc='docker compose'
alias dm='docker-machine'
alias e='echo'
alias g='git'
alias h='history'
alias m='make'
alias s='ssh'
alias sb='sensible-browser'
alias tf='terraform'
alias k='kubectl'
alias os=openstack

# Git aliases/func
alias gst='git status'
alias gpr='git pull --rebase'
alias grrh='git reset --hard HEAD'
alias gcdf='git clean -df'
alias gcam='git commit --amend'
alias gcamne='git commit --amend --no-edit'
alias grpo='git remote prune origin'
alias gspp='git stash && git pull --rebase && git stash pop'
alias glg='git lg --stat -C -4'
gri() { git rebase -i HEAD~$1; }

# Docker aliases/func
dps()  { docker ps -a --format 'table{{.Names}}\t{{.Status}}'; }
dpsa() { docker ps -a --format 'table{{.Names}}\t{{.Status}}\t{{.Image}}\t{{.Ports}}'; }
dim()  { docker images | grep $@; }
alias xd='xargs -r docker'
drmc() { docker ps -qa --filter 'status=dead' --filter 'status=exited' | xd rm; }
drmcg() { docker ps -a | grep "${@:-a}" | awk '{print $1}' | xargs -n1 docker rm -f; }
drmi() { docker images -q --filter "dangling=true" | xd rmi; }
drmig() { docker images | grep "${@:-a}" | awk '{printf "%s:%s\n", $1, $2}' | xargs -n1 docker rmi -f; }
drmv() { docker volume ls -q | xd volume rm; }
drmn() { docker network ls | awk '{print $1,$2}' | tail -n +2 | egrep -Ev "( docker_gwbridge$| bridge$| host$| none$| ingress$)" | awk '{print $1}' | xd network rm; }
drms() { docker service ls | tail -n +2 | awk '{print $2}' | xd service rm; }
dprune() { docker system prune -f }
drmall() { drmc; drmi; drmv; drmn; }
dkillall()  { docker ps -aq | xd rm -f; }
dstopall()  { docker ps -aq | xargs -r docker stop; }
dip()  { docker inspect --format '{{ .NetworkSettings }}' "$@"; }
dpid() { docker inspect --format '{{ .State.Pid }}' "$@"; }
dsh()  { docker exec -ti $@ sh }
dbash() { docker exec -ti $@ bash }
dzsh() { docker exec -ti $@ zsh }
dstats() { docker stats $(docker ps | grep -v CON | sed "s/.*\s\([a-z].*\)/\1/" | awk '{printf $1" "}'); }
dme()  { eval $(docker-machine env --shell zsh $1); }

# Kubectl aliases/func
alias kes="kubectl -n elastic-system"
alias wkes="watch -n1 kubectl get elastic,po"

kgetdel() { kubectl get $1 | grep $2 | awk '{print $1"\n"}' | xargs -n1 kubectl delete $1 --force; }

# Misc aliases/func

alias get='apk add'

# Tmux with terminal supports 256 colours
alias tmux='tmux -2'

# @help randpwd $length
randpwd() {
  local length=${1:-20}
  cat /dev/urandom | tr -dc 'a-zA-Z0-9&@#!%?;:-[]{}' | fold -w $length | head -1
}

# @help always $sleepDuration $cmd
always() { duration=$1 && shift; while true; do $@; sleep $duration; done; }

# @help grepcode $path $fileExtension $grepRegexp
grepcode() { find $1 -name "*.$2" | xargs grep -Hn $3; }

# cURL functions
curlv() { curl $@ -w "\n@status=%{response_code}\n@time=%{time_total}\n"; }
cl()    { curl -s localhost:$@; }
jc()    { curl -s $@ | jq .; }
jcl()   { curl -s localhost:$@ | jq .; }
kurl() {
  curl -sSL --connect-timeout 3 $@ -o /dev/null \
    -w '{"url":"%{url_effective}","status":"%{http_code}","time":"%{time_total}"}\n'
}

msg() { echo -n '{"user":"'$USER'@'$(hostname)'", "message":"'$@'"}'; }

# Source a .env file in 'VAR=value' format (used in the docker world)
sourcenv() {
  if [[ "$1" != "" ]]; then
    export $(cat $1 | xargs)
  else
    while read envfile; do
      echo "source $envfile"
      export $(cat $envfile | grep -vE "(^#|^\s*$)" | xargs)
    done < <(find . -name "*.env")
  fi
}

# Make subdirectories and move into them right away
mdir(){
  mkdir -p $1; cd $1
  echo "You are now in $PWD"
}

# found at http://stackoverflow.com/a/245724/452140
uup(){
  LIMIT=$1
  if [ -z "$LIMIT" ]; then
    LIMIT=1
  fi
  P=$PWD
  for ((i=1; i <= LIMIT; i++)); do
    P=$P/..
  done
  cd $P
  echo "You are now in $PWD"
}

# Search files given a pattern in a directory
# @what  pattern
# @where path
ff(){
  what=$1
  if [ $2 ]; then
    where=$2
  else
    where=""
  fi
  # redirect the stderr to stdout, grep both to exclude 'permission denied'
  # assumes we don't want to find a file that's called "permission denied".
  find $where -iname "$what" 2>&1 | grep -v 'Permission denied'
}

# --- xclip

# Copy stdin in the clipboard
xclip() {
  echo "$@" | xclip -selection c
}

# Copy current directory path in the clipboard
cppwd(){
  echo "cd $(pwd)" | xclip
}

# Copy last command in the clipboard
cplastcmd(){
  echo -n $(fc -nl -1) | tr -d '\n' | xclip
}

# Copy last command ouput in the clipboard
cplastcmdout(){
  cmd=$(fc -nl -1 | tr -d '\n')
  $cmd | xclip -sel clipboard
}
