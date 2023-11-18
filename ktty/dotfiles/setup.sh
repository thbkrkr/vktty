#!/bin/sh -eu
#
# Bootstrap dotfiles installation in docker (root)
#
# curl -sSL https://raw.github.com/thbkrkr/dotfiles/master/bootstrap/light | sh -s <sha1>

git clone https://github.com/robbyrussell/oh-my-zsh.git /root/.oh-my-zsh

mkdir -p /root/.vim/colors && mkdir /root/.vim/bundle}
git clone https://github.com/gmarik/Vundle.vim.git /root/.vim/bundle/Vundle.vim
curl -s https://raw.githubusercontent.com/sickill/vim-monokai/master/colors/monokai.vim \
    > /root/.vim/colors/monokai.vim

git clone https://github.com/thbkrkr/dotfiles.git /root/.dotfiles
git --git-dir=/root/.dotfiles/.git --work-tree=/root/.dotfiles checkout $1

cp /root/.dotfiles/resources/pure-thb.zsh-theme /root/.oh-my-zsh/themes/pure-thb.zsh-theme
find /root/.dotfiles -type f -name ".[a-z]*" -exec cp {} /root \;
sed -i "s|root:x:0:0:root:/root:/bin/ash|root:x:0:0:root:/root:/bin/zsh|" /etc/passwd