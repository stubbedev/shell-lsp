import { readdir, readFile } from "node:fs/promises";
import { join } from "node:path";
import { parseArgs } from "node:util";
import { releaseAssetProblems } from "../src/release.ts";

const {
  positionals: [dir, version, ...extra],
} = parseArgs({ allowPositionals: true, strict: true });
if (dir === undefined || version === undefined || extra.length > 0) {
  throw new Error("usage: node scripts/checkReleaseAssets.ts <directory just dist wrote> <version without the leading v>");
}

const problems = releaseAssetProblems(version, await readdir(dir), await readFile(join(dir, "SHA256SUMS"), "utf8"));
if (problems.length > 0) {
  throw new Error(`the release in ${dir} does not match what the VS Code extension downloads:\n${problems.join("\n")}`);
}
