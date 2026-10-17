# Editor setup

shell-lsp speaks LSP over stdio and needs no arguments. Put the binary on
`PATH` (release binaries, `nix profile install github:stubbedev/shell-lsp`, or
`go install github.com/stubbedev/shell-lsp@latest`) and point your editor at
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

To show which shell a buffer is linted as, and switch it, ask the server with
the `shellLsp/documentDialect` and `shellLsp/setDialect` requests (see
[Dialects](../README.md#dialects)). On Neovim 0.11 and later, with the
`shell_lsp` configuration above, this keeps the shell in the statusline, marked
with `?` when it is only guessed, and adds `:ShellDialect dash` to record a
shell as a `# shellcheck shell=` directive and `:ShellDialect` to remove it:

```lua
local function refresh_dialect(buf)
  for _, client in ipairs(vim.lsp.get_clients({ bufnr = buf, name = "shell_lsp" })) do
    client:request("shellLsp/documentDialect", { textDocument = vim.lsp.util.make_text_document_params(buf) }, function(err, result)
      if err == nil and result ~= nil then
        vim.b[buf].shell_dialect = result.guessed and (result.dialect .. "?") or result.dialect
        vim.b[buf].shell_dialect_why = result.description
        vim.cmd.redrawstatus()
      end
    end, buf)
  end
end

vim.api.nvim_create_autocmd({ "LspAttach", "BufEnter", "InsertLeave", "TextChanged" }, {
  callback = function(args)
    refresh_dialect(args.buf)
  end,
})

vim.api.nvim_create_user_command("ShellDialect", function(opts)
  local buf = vim.api.nvim_get_current_buf()
  local client = vim.lsp.get_clients({ bufnr = buf, name = "shell_lsp" })[1]
  if client == nil then
    return
  end
  local dialect = opts.args ~= "" and opts.args or vim.NIL
  local params = { textDocument = vim.lsp.util.make_text_document_params(buf), dialect = dialect }
  client:request("shellLsp/setDialect", params, function(err, edit)
    if err == nil and edit ~= nil then
      vim.lsp.util.apply_workspace_edit(edit, client.offset_encoding)
    end
  end, buf)
end, { nargs = "?" })

vim.o.statusline = "%f %{get(b:, 'shell_dialect', '')} %m"
```

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
Helix has no way to send the custom `shellLsp/documentDialect` request, so it
cannot show which shell a file is linted as; write a `# shellcheck shell=`
directive to choose one.

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

Zed extensions cannot send custom requests to a language server they do not
own, so Zed cannot show which shell a file is linted as either; a
`# shellcheck shell=` directive chooses it.

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
Prebuilt binaries cover Linux, macOS and Windows on x64 and arm64; on other
platforms install the server yourself and set `shellLsp.path`.

The server's settings are VS Code settings under `shellLsp`
(`shellLsp.dialect`, `shellLsp.lint.exclude`, `shellLsp.format.indent`, and
so on; the keys are listed in the README) and may differ per workspace folder.
Only values you set are sent to the server, so `.editorconfig` still decides
whatever you leave at its default. `shellLsp.trace.server` logs the protocol
traffic to the Shell LSP output. The commands "Shell LSP: Restart Server",
"Shell LSP: Show Output", "Shell LSP: Fix All Auto-Fixable Problems",
"Shell LSP: Run Bats File" and "Shell LSP: Choose the Shell to Lint As" are
in the command palette; reference-count code lenses open the references peek.
The status bar shows the shell the active script is linted as and what
decided it, and clicking it chooses another. More in the extension's
[README](../editors/vscode/README.md).

## Any other client

Run `shell-lsp` over stdio for shell buffers. It negotiates UTF-8, UTF-16 or
UTF-32 positions, uses pull diagnostics when the client supports them and
pushes them otherwise.

## Shell in other files

shell-lsp also lints the shell inside Makefile and justfile recipes and Nix
strings ([Shell in other files](../README.md#shell-in-other-files)) when the
editor sends it those files; it reads the language identifiers `makefile`,
`make`, `just` and `nix`. For justfiles and Makefiles it is a complete
language server of its own ([Justfiles and Makefiles natively](../README.md#justfiles-and-makefiles-natively)):
attach it alone, without just-lsp or a make language server, so the file gets
one set of diagnostics, symbols and formatting. The VS Code extension attaches to Makefiles on its
own, and to justfiles and Nix files when extensions give them the `just` and
`nix` languages. Elsewhere, add the file types next to the shell ones. In
Neovim, add `"make"`, `"just"` and `"nix"` to `filetypes` (or to the
`FileType` pattern); shell-lsp then runs next to any Nix language server:

```lua
vim.lsp.config("shell_lsp", {
  cmd = { "shell-lsp" },
  filetypes = { "sh", "bash", "zsh", "ksh", "make", "just", "nix" },
  root_markers = { ".git" },
})
```

In Helix, list shell-lsp for the `make`, `just` and `nix` languages, next to
the language servers they already use:

```toml
[[language]]
name = "make"
language-servers = ["shell-lsp"]

[[language]]
name = "just"
language-servers = ["shell-lsp"]

[[language]]
name = "nix"
language-servers = ["nil", "shell-lsp"]
```

## Windows

The Windows binary is `shell-lsp.exe`; put it on `PATH` or point the
editor at it. Everything else works as on Linux and macOS, with these
differences:

- Document URIs may name a drive (`file:///c%3A/...`) or a UNC share
  (`file://server/share/...`), and paths compare case-insensitively.
- Command completion lists the programs on `PATH` whose extension is in
  `PATHEXT`, without the extension (`git` for `git.exe`).
- Man pages, for hover and option completion, come from `MANPATH`
  (separated by `;`) and from the `share\man` and `man` directories next to
  each `PATH` entry, which covers the trees MSYS2 and Git for Windows
  install. There are no further default locations.
- Bats tests run through the `bash.exe` of Git for Windows or MSYS2 found on
  `PATH`. The WSL launcher in `System32` is skipped because it cannot see
  Windows paths. A `bats.cmd` or `bats.exe` launcher, such as the one npm
  installs, runs directly. Each run is placed in its own job object, so
  stopping it, or the server exiting, ends every process the suite started.
