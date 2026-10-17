export const displayName = "Shell LSP";
export const repository = "https://github.com/stubbedev/shell-lsp";

export const clientCommands = {
  restart: "shellLsp.restart",
  showOutput: "shellLsp.showOutput",
  chooseShell: "shellLsp.chooseShell",
} as const;

export const serverCommands = {
  fixAll: "shellLsp.fixAll",
  applyEdit: "shellLsp.applyEdit",
  showReferences: "shellLsp.showReferences",
  runBats: "shellLsp.runBats",
  runRecipe: "shellLsp.runRecipe",
} as const;

export type ClientCommand = (typeof clientCommands)[keyof typeof clientCommands];
export type ServerCommand = (typeof serverCommands)[keyof typeof serverCommands];
export type Command = ClientCommand | ServerCommand;

export const runningContext = "shellLsp.running";
export const dialectStatusItem = "shellLsp.dialect";
