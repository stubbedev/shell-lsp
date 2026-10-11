import { build, type BuildOptions } from "esbuild";
import { join } from "node:path";
import { parseArgs } from "node:util";

const { values } = parseArgs({
  options: {
    production: { type: "boolean", default: false },
    tests: { type: "boolean", default: false },
  },
  strict: true,
});

const root = join(import.meta.dirname, "..");

const shared: BuildOptions = {
  bundle: true,
  platform: "node",
  format: "cjs",
  target: "node20",
  external: ["vscode"],
  logLevel: "warning",
  minify: values.production,
  sourcemap: !values.production,
};

const bundles: BuildOptions[] = [
  { entryPoints: [join(root, "src", "extension.ts")], outfile: join(root, "dist", "extension.cjs") },
];
if (values.tests) {
  bundles.push({
    entryPoints: [join(root, "test", "integration", "suite.ts")],
    outfile: join(root, "out", "integration", "suite.cjs"),
  });
}

await Promise.all(bundles.map((bundle) => build({ ...shared, ...bundle })));
