import { join } from "node:path";
import { commands, ProgressLocation, window, workspace, type ExtensionContext, type LogOutputChannel } from "vscode";
import { State, type LanguageClient } from "vscode-languageclient/node";
import { createClient } from "./client.ts";
import { describeError } from "./errors.ts";
import { httpFetcher } from "./http.ts";
import { installServer } from "./install.ts";
import { extensionVersion, releaseAsset, releaseTarget, type ReleaseAsset } from "./release.ts";
import { findServer, isExecutableFile, serverName } from "./serverBinary.ts";
import { pathKey, pathSetting, section, serverKeys } from "./settings.ts";

const runningContext = "shellLsp.running";
const downloadLimits = { timeoutMs: 300_000, maxBytes: 256 * 1024 * 1024 };

export class Session {
  private readonly output: LogOutputChannel;
  private readonly storageDir: string;
  private readonly asset: ReleaseAsset | undefined;
  private readonly settingKeys: readonly string[];
  private client: LanguageClient | undefined;
  private queue: Promise<void> = Promise.resolve();

  constructor(context: ExtensionContext, output: LogOutputChannel) {
    this.output = output;
    this.storageDir = context.globalStorageUri.fsPath;
    const manifest: unknown = context.extension.packageJSON;
    this.settingKeys = serverKeys(manifest);
    const target = releaseTarget(process.platform, process.arch);
    this.asset = target === undefined ? undefined : releaseAsset(extensionVersion(manifest), target);
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
        downloaded: this.asset === undefined ? undefined : join(this.storageDir, this.asset.name),
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
    await commands.executeCommand("setContext", runningContext, false);
    await client.dispose();
  }

  private async offerDownload(): Promise<void> {
    const settings = "Open Settings";
    if (this.asset === undefined) {
      const choice = await window.showErrorMessage(
        `There is no prebuilt ${serverName} for ${process.platform}-${process.arch}. Install ${serverName} manually and set ${pathSetting}.`,
        settings,
      );
      if (choice === settings) {
        await this.openSettings();
      }
      return;
    }
    const download = "Download";
    const choice = await window.showInformationMessage(
      `${serverName} was not found on PATH and ${pathSetting} is not set. Download ${this.asset.name} from its GitHub release?`,
      download,
      settings,
    );
    if (choice === settings) {
      await this.openSettings();
      return;
    }
    if (choice !== download) {
      return;
    }
    const asset = this.asset;
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
