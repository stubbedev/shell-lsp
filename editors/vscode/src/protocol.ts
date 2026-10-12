import type { Location, Position, Range, TextDocumentEdit, TextEdit, WorkspaceEdit } from "vscode-languageclient";
import { isFields, isList } from "./fields.ts";

export function isPosition(value: unknown): value is Position {
  return isFields(value) && Number.isInteger(value["line"]) && Number.isInteger(value["character"]);
}

export function isRange(value: unknown): value is Range {
  return isFields(value) && isPosition(value["start"]) && isPosition(value["end"]);
}

export function isLocation(value: unknown): value is Location {
  return isFields(value) && typeof value["uri"] === "string" && isRange(value["range"]);
}

function isTextEdit(value: unknown): value is TextEdit {
  return isFields(value) && isRange(value["range"]) && typeof value["newText"] === "string";
}

function isTextDocumentEdit(value: unknown): value is TextDocumentEdit {
  if (!isFields(value) || !isFields(value["textDocument"]) || !isList(value["edits"], isTextEdit)) {
    return false;
  }
  const { uri, version } = value["textDocument"];
  return typeof uri === "string" && (version === null || Number.isInteger(version));
}

function isTextEditsByUri(value: unknown): value is Readonly<Record<string, readonly TextEdit[]>> {
  return isFields(value) && Object.values(value).every((edits) => isList(edits, isTextEdit));
}

export function isTextWorkspaceEdit(value: unknown): value is WorkspaceEdit {
  if (!isFields(value)) {
    return false;
  }
  const { changes, documentChanges } = value;
  if (changes === undefined && documentChanges === undefined) {
    return false;
  }
  return (changes === undefined || isTextEditsByUri(changes)) && (documentChanges === undefined || isList(documentChanges, isTextDocumentEdit));
}
