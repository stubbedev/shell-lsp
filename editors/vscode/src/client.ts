import { workspace, type LogOutputChannel, type Uri } from "vscode";
import { LanguageClient, ResponseError, type LanguageClientOptions } from "vscode-languageclient/node";
import { executeCommandMiddleware } from "./commands.ts";
import { documentSelector } from "./documents.ts";
import { displayName } from "./ids.ts";
import { explicitSettings, section } from "./settings.ts";

export function createClient(command: string, output: LogOutputChannel, settingKeys: readonly string[]): LanguageClient {
  const settingsFor = (scope: Uri | undefined): Record<string, unknown> => {
    const configuration = workspace.getConfiguration(section, scope);
    return explicitSettings(settingKeys, (key) => configuration.inspect(key));
  };
  const options: LanguageClientOptions = {
    documentSelector,
    outputChannel: output,
    synchronize: { configurationSection: section },
    initializationOptions: () => settingsFor(undefined),
    middleware: {
      executeCommand: executeCommandMiddleware(() => client),
      workspace: {
        didChangeConfiguration: (_sections, next) => next(undefined),
        configuration: async (params, token, next) => {
          const result = await next(params, token);
          if (result instanceof ResponseError) {
            return result;
          }
          const values: readonly unknown[] = result;
          return values.map((value, index) => {
            const item = params.items[index];
            if (item?.section !== section) {
              return value;
            }
            return settingsFor(item.scopeUri === undefined ? undefined : client.protocol2CodeConverter.asUri(item.scopeUri));
          });
        },
      },
    },
  };
  const client = new LanguageClient(section, displayName, { command }, options);
  return client;
}
