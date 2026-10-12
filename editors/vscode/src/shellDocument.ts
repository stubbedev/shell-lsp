import { languages, window, type TextDocument } from "vscode";
import type { LanguageClient, TextDocumentIdentifier } from "vscode-languageclient/node";
import { documentSelector, languageIds } from "./documents.ts";
import type { Command } from "./ids.ts";

export interface ShellDocument {
  readonly document: TextDocument;
  readonly textDocument: TextDocumentIdentifier;
}

export function activeShellDocument(client: LanguageClient): ShellDocument | undefined {
  const document = window.activeTextEditor?.document;
  if (document === undefined || languages.match(documentSelector, document) === 0) {
    return undefined;
  }
  return { document, textDocument: { uri: client.code2ProtocolConverter.asUri(document.uri) } };
}

export function requireActiveShellDocument(client: LanguageClient, command: Command): ShellDocument {
  const found = activeShellDocument(client);
  if (found === undefined) {
    throw new Error(`${command} needs an active editor on a document of one of the languages ${languageIds.join(", ")}`);
  }
  return found;
}
