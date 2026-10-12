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
} as const;

export type ServerCommand = (typeof serverCommands)[keyof typeof serverCommands];

export const runningContext = "shellLsp.running";
