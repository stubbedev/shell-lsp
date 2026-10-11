# Editor setup

shell-lsp speaks LSP over stdio and needs no arguments. Put the binary on
`PATH` (release binaries, `nix profile install github:stubbedev/shell-lsp`, or
`go install .` in a clone of the repository) and point your editor at
`shell-lsp`; the VS Code extension can also download it for you. Settings go into `initializationOptions` or the `shellLsp`
configuration section; see the README for the keys.

## Neovim

Neovim 0.11 and later:

```lua
vim.lsp.config("shell_lsp", {
  cmd = { "shell-lsp" },
  filetypes = { "sh", "bash", "zsh", "ksh" },
  root_markers = { ".git" },
})
vim.lsp.enable("shell_lsp")
```

Neovim 0.9 and 0.10, from an `FileType` autocommand:

```lua
vim.api.nvim_create_autocmd("FileType", {
  pattern = { "sh", "bash", "zsh", "ksh" },
  callback = function(args)
    vim.lsp.start({
      name = "shell-lsp",
      cmd = { "shell-lsp" },
      root_dir = vim.fs.root(args.buf, { ".git" }) or vim.fn.getcwd(),
    })
  end,
})
```

`vim.fs.root` needs 0.10; on 0.9 use
`vim.fs.dirname(vim.fs.find({ ".git" }, { upward = true })[1])`.
The repository's `scripts/nvim-smoke.lua` exercises the 0.9 form in CI.

## Helix

`~/.config/helix/languages.toml`:

```toml
[language-server.shell-lsp]
command = "shell-lsp"

[[language]]
name = "bash"
language-servers = ["shell-lsp"]
```

Helix's `bash` language covers `.sh`, `.bash`, `.zsh` and the usual rc files;
shell-lsp detects the actual dialect per file.

## Zed

Zed's built-in shell support launches `bash-language-server`; override its
binary in `settings.json`:

```json
{
  "lsp": {
    "bash-language-server": {
      "binary": { "path": "shell-lsp", "arguments": [] }
    }
  }
}
```

## VS Code

Install the Shell LSP extension, `stubbedev.shell-lsp`, in any of these ways:

- from the Visual Studio Marketplace: search for "Shell LSP" in the
  Extensions view, or run `code --install-extension stubbedev.shell-lsp`;
- from [Open VSX](https://open-vsx.org) in VSCodium and other editors that use
  it, under the same name;
- from a release: download `shell-lsp-<version>.vsix` from the
  [release page](https://github.com/stubbedev/shell-lsp/releases) and run
  `code --install-extension shell-lsp-<version>.vsix`, or pick "Install from
  VSIX..." in the Extensions view's `...` menu.

The extension starts the server for Shell Script documents and for `.bats`
files and `bats` shebangs. It runs the binary named by `shellLsp.path` (an
absolute path, set in user settings), else `shell-lsp` on `PATH`, else a
binary it downloaded earlier. When it finds none it offers to download the
release binary for its own version, verifies it against the release's
`SHA256SUMS` before saving it, and keeps it in the extension's storage.
Prebuilt binaries cover Linux and macOS on x64 and arm64; on other platforms
install the server yourself and set `shellLsp.path`.

The server's settings are VS Code settings under `shellLsp`
(`shellLsp.dialect`, `shellLsp.lint.exclude`, `shellLsp.format.indent`, and
so on; the keys are listed in the README) and may differ per workspace folder.
Only values you set are sent to the server, so `.editorconfig` still decides
whatever you leave at its default. `shellLsp.trace.server` logs the protocol
traffic to the Shell LSP output. The commands "Shell LSP: Restart Server",
"Shell LSP: Show Output", "Shell LSP: Fix All Auto-Fixable Problems" and
"Shell LSP: Run Bats File" are in the command palette; reference-count code
lenses open the references peek. More in the extension's
[README](../editors/vscode/README.md).

## Any other client

Run `shell-lsp` over stdio for shell buffers. It negotiates UTF-8, UTF-16 or
UTF-32 positions, uses pull diagnostics when the client supports them and
pushes them otherwise.
