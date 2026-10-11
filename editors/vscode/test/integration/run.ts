import { runTests } from "@vscode/test-electron";
import { cp, mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const [binaryArgument] = process.argv.slice(2);
if (binaryArgument === undefined) {
  throw new Error("usage: node test/integration/run.ts <path of the shell-lsp binary>");
}
const binary = resolve(binaryArgument);
const extensionRoot = resolve(import.meta.dirname, "..", "..");
const vscodeExecutable = process.env["SHELL_LSP_VSCODE_EXECUTABLE"];

const sandbox = await mkdtemp(join(tmpdir(), "shell-lsp-vscode-"));
try {
  const workspaceDir = join(sandbox, "workspace");
  const userDataDir = join(sandbox, "user-data");
  await cp(join(import.meta.dirname, "workspace"), workspaceDir, { recursive: true });
  await mkdir(join(userDataDir, "User"), { recursive: true });
  await writeFile(
    join(userDataDir, "User", "settings.json"),
    JSON.stringify({ "shellLsp.path": binary, "shellLsp.trace.server": "messages" }),
  );
  await runTests({
    extensionDevelopmentPath: extensionRoot,
    extensionTestsPath: join(extensionRoot, "out", "integration", "suite.cjs"),
    launchArgs: [
      workspaceDir,
      "--disable-extensions",
      "--disable-workspace-trust",
      `--user-data-dir=${userDataDir}`,
      `--extensions-dir=${join(sandbox, "extensions")}`,
    ],
    ...(vscodeExecutable === undefined ? {} : { vscodeExecutablePath: vscodeExecutable }),
  });
} finally {
  await rm(sandbox, { recursive: true, force: true });
}
