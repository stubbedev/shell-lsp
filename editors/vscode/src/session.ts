import { join } from "node:path";
import { commands, ProgressLocation, window, workspace, type ExtensionContext, type LogOutputChannel } from "vscode";
import { State, type LanguageClient } from "vscode-languageclient/node";
import { createClient } from "./client.ts";
import { describeError } from "./errors.ts";
import { httpFetcher } from "./http.ts";
import { runningContext } from "./ids.ts";
import { installServer } from "./install.ts";
import { parseManifest } from "./manifest.ts";
import { serverDownload, type ReleaseAsset, type ServerDownload } from "./release.ts";
import { findServer, isExecutableFile, serverName } from "./serverBinary.ts";
import { pathKey, pathSetting, section } from "./settings.ts";

const downloadLimits = { timeoutMs: 300_000, maxBytes: 256 * 1024 * 1024 };
const openSettingsLabel = "Open Settings";

export class Session {
  private readonly output: LogOutputChannel;
  private readonly storageDir: string;
  private readonly download: ServerDownload;
  private readonly settingKeys: readonly string[];
  private readonly onStateChange: () => void;
  private client: LanguageClient | undefined;
  private queue: Promise<void> = Promise.resolve();

  constructor(context: ExtensionContext, output: LogOutputChannel, onStateChange: () => void) {
    this.output = output;
    this.onStateChange = onStateChange;
    this.storageDir = context.globalStorageUri.fsPath;
    const manifest = parseManifest(context.extension.packageJSON);
    this.settingKeys = manifest.serverKeys;
    this.download = serverDownload(manifest.version, process.platform, process.arch);
  }

  runningClient(): LanguageClient | undefined {
    return this.client?.state === State.Running ? this.client : undefined;
  }

  start(): Promise<void> {
    return this.enqueue(() => this.launch());
  }

  restart(): Promise<void> {
    return this.enqueue(async () => {
      await this.shutdown();
      await this.launch();
    });
  }

  stop(): Promise<void> {
    return this.enqueue(() => this.shutdown());
  }

  private enqueue(task: () => Promise<void>): Promise<void> {
    const run = this.queue.then(task).catch((error: unknown) => {
      this.report(error);
    });
    this.queue = run;
    return run;
  }

  private async launch(): Promise<void> {
    const binary = await findServer(
      {
        platform: process.platform,
        configuredPath: configuredPath(),
        pathEnv: process.env["PATH"],
        downloaded: this.download.kind === "release" ? join(this.storageDir, this.download.asset.name) : undefined,
      },
      isExecutableFile,
    );
    if (binary === undefined) {
      this.offerDownload().catch((error: unknown) => {
        this.report(error);
      });
      return;
    }
    this.output.info(`Starting ${binary.path}, found via ${binary.source}`);
    const client = createClient(binary.path, this.output, this.settingKeys);
    client.onDidChangeState((change) => {
      void commands.executeCommand("setContext", runningContext, change.newState === State.Running);
      this.onStateChange();
    });
    this.client = client;
    await client.start();
  }

  private async shutdown(): Promise<void> {
    const client = this.client;
    if (client === undefined) {
      return;
    }
    this.client = undefined;
    this.onStateChange();
    await commands.executeCommand("setContext", runningContext, false);
    await client.dispose();
  }

  private async offerDownload(): Promise<void> {
    switch (this.download.kind) {
      case "unsupported":
        await this.offerManualInstall(this.download.platform);
        return;
      case "release":
        await this.offerRelease(this.download.asset);
        return;
    }
  }

  private async offerManualInstall(platform: string): Promise<void> {
    const choice = await window.showErrorMessage(
      `There is no prebuilt ${serverName} for ${platform}. Install ${serverName} manually and set ${pathSetting}.`,
      openSettingsLabel,
    );
    if (choice === openSettingsLabel) {
      await this.openSettings();
    }
  }

  private async offerRelease(asset: ReleaseAsset): Promise<void> {
    const download = "Download";
    const choice = await window.showInformationMessage(
      `${serverName} was not found on PATH and ${pathSetting} is not set. Download ${asset.name} from its GitHub release?`,
      download,
      openSettingsLabel,
    );
    if (choice === openSettingsLabel) {
      await this.openSettings();
      return;
    }
    if (choice !== download) {
      return;
    }
    const installed = await window.withProgress(
      { location: ProgressLocation.Notification, title: `Downloading ${asset.name}` },
      () => installServer(asset, this.storageDir, httpFetcher(downloadLimits)),
    );
    this.output.info(`Installed ${installed}`);
    await this.start();
  }

  private async openSettings(): Promise<void> {
    await commands.executeCommand("workbench.action.openSettings", pathSetting);
  }

  private report(error: unknown): void {
    const message = describeError(error);
    this.output.error(message);
    const show = "Show Output";
    void window.showErrorMessage(`Shell LSP: ${message}`, show).then((choice) => {
      if (choice === show) {
        this.output.show();
      }
    });
  }
}

function configuredPath(): string {
  const value: unknown = workspace.getConfiguration(section).get(pathKey);
  if (typeof value !== "string") {
    throw new Error(`${pathSetting} must be a string, got ${JSON.stringify(value)}`);
  }
  return value;
}
