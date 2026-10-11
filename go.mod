module github.com/stubbedev/shell-lsp

go 1.27.1

require mvdan.cc/sh/v3 v3.14.1

replace mvdan.cc/sh/v3 => ./third_party/mvdan-sh

ignore ./editors/vscode/node_modules
