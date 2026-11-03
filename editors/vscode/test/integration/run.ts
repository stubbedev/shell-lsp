import { runTests } from "@vscode/test-electron";
import { access, cp, mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
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
  const finished = join(sandbox, "suite-finished");
  await cp(join(import.meta.dirname, "workspace"), workspaceDir, { recursive: true });
  await mkdir(join(userDataDir, "User"), { recursive: true });
  await writeFile(
    join(userDataDir, "User", "settings.json"),
    JSON.stringify({ "shellLsp.path": binary, "shellLsp.trace.server": "messages" }),
  );
  await runTests({
    extensionDevelopmentPath: extensionRoot,
    extensionTestsPath: join(extensionRoot, "out", "integration", "suite.cjs"),
    extensionTestsEnv: { SHELL_LSP_SUITE_FINISHED: finished },
    launchArgs: [
      workspaceDir,
      "--disable-extensions",
      "--disable-workspace-trust",
      `--user-data-dir=${userDataDir}`,
      `--extensions-dir=${join(sandbox, "extensions")}`,
    ],
    ...(vscodeExecutable === undefined ? {} : { vscodeExecutablePath: vscodeExecutable }),
  });
  await access(finished).catch((error: unknown) => {
    throw new Error(`${vscodeExecutable ?? "VS Code"} exited without running the suite; SHELL_LSP_VSCODE_EXECUTABLE must name the Electron binary, not a launcher script`, { cause: error });
  });
} finally {
  await rm(sandbox, { recursive: true, force: true });
}
