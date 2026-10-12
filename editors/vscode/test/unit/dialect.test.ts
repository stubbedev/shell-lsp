import assert from "node:assert/strict";
import { describe, test } from "node:test";
import {
  directiveEdit,
  documentDialect,
  hostExplanation,
  pickPlaceholder,
  shellArgument,
  shellPicks,
  statusText,
  type HostDialect,
  type ScriptDialect,
} from "../../src/dialect.ts";

const dialects = ["bash", "sh", "dash", "zsh", "ksh", "busybox"];
const range = { start: { line: 1, character: 0 }, end: { line: 1, character: 0 } };

const fromShebang: ScriptDialect = {
  selectable: true,
  dialect: "bash",
  source: "shebang",
  guessed: false,
  description: "from the shebang #!/usr/bin/env bash",
  directive: false,
  dialects,
};

const guessed: ScriptDialect = {
  selectable: true,
  dialect: "sh",
  source: "extension",
  guessed: true,
  description: "guessed from the .sh extension",
  directive: false,
  dialects,
};

const fromMakefile: HostDialect = {
  selectable: false,
  dialect: "bash",
  source: "hostSetting",
  guessed: false,
  description: "from SHELL := /bin/bash",
};

describe("documentDialect", () => {
  test("reads the server's answer for a script", () => {
    assert.deepEqual(documentDialect({ ...fromShebang, evidence: "#!/usr/bin/env bash" }), fromShebang);
  });

  test("reads the server's answer for shell whose host file decides it", () => {
    assert.deepEqual(documentDialect({ ...fromMakefile, evidence: "SHELL := /bin/bash" }), fromMakefile);
  });

  test("rejects anything else, naming the request", () => {
    for (const result of [
      null,
      "bash",
      { dialect: "bash", source: "shebang", guessed: false, description: "from the shebang", directive: false, dialects },
      { ...fromShebang, selectable: "yes" },
      { ...fromShebang, guessed: "no" },
      { ...fromShebang, dialects: [1] },
      { ...fromShebang, source: "hostSetting" },
      { ...fromShebang, source: "somewhere" },
      { ...fromShebang, directive: undefined },
      { ...fromMakefile, source: "shebang" },
      { dialect: "bash" },
    ]) {
      assert.throws(() => documentDialect(result), /^Error: shellLsp\/documentDialect answered /);
    }
  });
});

describe("directiveEdit", () => {
  test("passes edits and the null that means nothing to change", () => {
    assert.equal(directiveEdit(null), null);
    const edit = { documentChanges: [] };
    assert.equal(directiveEdit(edit), edit);
    const changes = { changes: {} };
    assert.equal(directiveEdit(changes), changes);
  });

  test("passes the text edits the server answers", () => {
    const answer = {
      documentChanges: [{ textDocument: { uri: "file:///w/a.sh", version: 3 }, edits: [{ range, newText: "# shellcheck shell=dash\n" }] }],
    };
    assert.equal(directiveEdit(answer), answer);
    const unversioned = { changes: { "file:///w/a.sh": [{ range, newText: "" }] } };
    assert.equal(directiveEdit(unversioned), unversioned);
  });

  test("rejects anything else", () => {
    for (const result of [
      undefined,
      "edit",
      [],
      {},
      { documentChanges: {} },
      { documentChanges: [{ textDocument: { uri: "file:///w/a.sh", version: 1 }, edits: [{ newText: "x" }] }] },
      { documentChanges: [{ textDocument: { uri: "file:///w/a.sh", version: "1" }, edits: [] }] },
      { documentChanges: [{ kind: "create", uri: "file:///w/b.sh" }] },
      { changes: { "file:///w/a.sh": [{ range, newText: 1 }] } },
      { changes: { "file:///w/a.sh": {} } },
    ]) {
      assert.throws(() => directiveEdit(result), /^Error: shellLsp\/setDialect answered /);
    }
  });
});

describe("shellArgument", () => {
  test("no argument asks, a name sets, null removes", () => {
    assert.equal(shellArgument("c", []), undefined);
    assert.equal(shellArgument("c", ["dash"]), "dash");
    assert.equal(shellArgument("c", [null]), null);
  });

  test("rejects other arguments", () => {
    for (const args of [[1], ["dash", "zsh"], [{ dialect: "dash" }]]) {
      assert.throws(() => shellArgument("shellLsp.chooseShell", args), /^Error: shellLsp\.chooseShell takes no argument/);
    }
  });
});

describe("statusText", () => {
  test("names the shell and why", () => {
    assert.deepEqual(statusText(fromShebang), {
      text: "bash",
      tooltip: "Linted as bash: from the shebang #!/usr/bin/env bash. Click to choose the shell.",
    });
  });

  test("marks a guess", () => {
    assert.equal(statusText(guessed).text, "sh (guessed)");
  });

  test("offers to explain a shell the host file decides", () => {
    assert.deepEqual(statusText(fromMakefile), {
      text: "bash",
      tooltip: "Linted as bash: from SHELL := /bin/bash. Click to see where the shell comes from.",
    });
  });
});

describe("hostExplanation", () => {
  test("says what decided the shell and where to change it", () => {
    const explanation = hostExplanation(fromMakefile);
    assert.match(explanation, /^Linted as bash, from SHELL := \/bin\/bash\. /);
    assert.match(explanation, /SHELL in a Makefile/);
    assert.match(explanation, /set shell in a justfile/);
    assert.match(explanation, /in a Nix file/);
    assert.match(explanation, /# shellcheck shell= directive cannot/);
  });
});

describe("shellPicks", () => {
  test("offers every shell, marks the current one and links the docs", () => {
    const picks = shellPicks(guessed);
    assert.deepEqual(
      picks.map((pick) => pick.label),
      [...dialects, "How is the shell detected?"],
    );
    assert.deepEqual(
      picks.filter((pick) => pick.description === "current").map((pick) => pick.label),
      ["sh"],
    );
    assert.deepEqual(picks[2]?.choice, { kind: "set", dialect: "dash" });
  });

  test("offers removal only when a directive exists", () => {
    const picks = shellPicks({ ...fromShebang, source: "directive", directive: true });
    assert.ok(picks.some((pick) => pick.choice.kind === "remove"));
    assert.ok(!shellPicks(fromShebang).some((pick) => pick.choice.kind === "remove"));
  });
});

describe("pickPlaceholder", () => {
  test("warns when a setting or .editorconfig decides instead of directives", () => {
    assert.match(pickPlaceholder({ ...fromShebang, source: "override" }), /shellLsp\.dialect setting/);
    assert.match(pickPlaceholder({ ...fromShebang, source: "editorconfig" }), /\.editorconfig/);
    assert.match(pickPlaceholder(guessed), /^Linted as sh, guessed from the \.sh extension\./);
  });
});
