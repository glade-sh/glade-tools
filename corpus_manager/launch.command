#!/bin/zsh
set -eu
export PATH="$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin:$PATH"
cd "${0:A:h:h}"
mkdir -p "$HOME/.local/share/glade-corpus-manager"
chmod 700 "$HOME/.local/share/glade-corpus-manager"
exec "${GLADE_CORPUS_PYTHON:-python3}" -m corpus_manager --open
