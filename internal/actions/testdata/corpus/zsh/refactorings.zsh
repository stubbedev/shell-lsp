#!/bin/zsh
setopt ERR_EXIT extended_glob

name=demo
if [[ $1 == build ]]; then make; elif [[ $1 == clean ]]; then rm -rf out; fi

greet() {
  if [[ $name = demo ]]; then
    print -r -- "demo"
  elif [[ $name = (x|y) ]]; then
    print other
  fi
}

greet
print -r -- "$name"
