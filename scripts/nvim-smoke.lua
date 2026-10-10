local server = assert(arg[1], "usage: nvim --headless -l scripts/nvim-smoke.lua <shell-lsp binary>")

local function fail(message)
  io.stderr:write("nvim smoke test failed: " .. message .. "\n")
  os.exit(1)
end

local dir = vim.fn.tempname()
vim.fn.mkdir(dir, "p")
local file = dir .. "/smoke.bash"
vim.fn.writefile({
  "#!/usr/bin/env bash",
  "# Greets someone.",
  "greet() { local who=\"$1\"; echo \"hi $who\"; }",
  "greet world",
  "if true; then",
}, file)

vim.cmd.edit(file)
local bufnr = vim.api.nvim_get_current_buf()
local client_id = vim.lsp.start({
  name = "shell-lsp",
  cmd = { server },
  root_dir = dir,
  init_options = { shellcheck = { enable = false } },
})
if not client_id then
  fail("vim.lsp.start returned no client")
end

local got_diagnostic = vim.wait(10000, function()
  for _, d in ipairs(vim.diagnostic.get(bufnr)) do
    if d.message:find("missing `fi`", 1, true) then
      return true
    end
  end
  return false
end, 50)
if not got_diagnostic then
  fail("no 'missing `fi`' diagnostic; got " .. vim.inspect(vim.diagnostic.get(bufnr)))
end

local function request(method, params)
  local responses = vim.lsp.buf_request_sync(bufnr, method, params, 5000)
  if not responses then
    fail(method .. " timed out")
  end
  for _, response in pairs(responses) do
    if response.err then
      fail(method .. " failed: " .. vim.inspect(response.err))
    end
    return response.result
  end
  fail(method .. " returned no response")
end

local uri = vim.uri_from_bufnr(bufnr)
local position = { textDocument = { uri = uri }, position = { line = 3, character = 1 } }

local hover = request("textDocument/hover", position)
if not hover or not hover.contents.value:find("greet who", 1, true) then
  fail("hover on the call lacks the signature: " .. vim.inspect(hover))
end

local definition = request("textDocument/definition", position)
if not definition or #definition ~= 1 or definition[1].range.start.line ~= 2 then
  fail("definition did not reach line 3: " .. vim.inspect(definition))
end

local completion = request("textDocument/completion", { textDocument = { uri = uri }, position = { line = 3, character = 2 } })
local found = false
for _, item in ipairs(completion.items or completion) do
  if item.label == "greet" then
    found = true
  end
end
if not found then
  fail("completion of 'gr' does not offer greet")
end

vim.lsp.stop_client(client_id)
print("nvim smoke test passed")
os.exit(0)
