# Editor setup

shell-lsp speaks LSP over stdio and needs no arguments. Put the binary on
`PATH` (release binaries, `nix profile install github:stubbedev/shell-lsp`, or
`go install github.com/stubbedev/shell-lsp@latest`) and point your editor at
`shell-lsp`. Settings go into `initializationOptions` or the `shellLsp`
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

VS Code only talks to language servers through extensions and shell-lsp does
not ship one yet. Use a generic LSP client extension that can launch an
arbitrary stdio server for the `shellscript` language, configured with the
command `shell-lsp`.

## Any other client

Run `shell-lsp` over stdio for shell buffers. It negotiates UTF-8, UTF-16 or
UTF-32 positions, uses pull diagnostics when the client supports them and
pushes them otherwise.
