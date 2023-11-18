#!/bin/bash -eu
#
# Install and configure Zsh, oh-my-zsh and dotfiles.
#

echo "Install .dotfiles..."

SRCDIR=$HOME/.home

# Clone or update oh-my-zsh
ohMyZshDir=$HOME/.oh-my-zsh
if [ ! -d $ohMyZshDir ]; then
  git clone -q https://github.com/robbyrussell/oh-my-zsh.git $ohMyZshDir
else
  git --git-dir=$ohMyZshDir/.git --work-tree=$ohMyZshDir pull -q --rebase
fi

# Copy custom theme
cp -f $SRCDIR/resources/pure-thb.zsh-theme $ohMyZshDir/themes/

# Clone or update Vundle.vim
mkdir -p $HOME/.vim/bundle
vundleDir=$HOME/.vim/bundle/Vundle.vim
if [ ! -d $vundleDir ]; then
  git clone -q https://github.com/gmarik/Vundle.vim.git $vundleDir
else
  git --git-dir=$vundleDir/.git --work-tree=$vundleDir pull -q --rebase
fi
# Install monokai colors
mkdir -p $HOME/.vim/colors
curl -s https://raw.githubusercontent.com/sickill/vim-monokai/master/colors/monokai.vim \
  > $HOME/.vim/colors/monokai.vim

# Copy all dotfiles
cp -f $SRCDIR/dotfiles/.[a-z]* $HOME/

# Copy all utils scripts
mv $SRCDIR/bin/* /usr/local/bin/
rmdir $SRCDIR/bin
