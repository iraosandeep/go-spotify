// Bundle the React UI into web/dist, which the Go binary embeds.
import tailwind from "bun-plugin-tailwind";
import { rm } from "node:fs/promises";

const outdir = new URL("./dist", import.meta.url).pathname;
await rm(outdir, { recursive: true, force: true });

const result = await Bun.build({
  entrypoints: [new URL("./src/index.html", import.meta.url).pathname],
  outdir,
  plugins: [tailwind],
  minify: true,
  publicPath: "/",
  target: "browser",
  define: { "process.env.NODE_ENV": JSON.stringify("production") },
});

if (!result.success) {
  for (const log of result.logs) console.error(log);
  process.exit(1);
}
for (const o of result.outputs) console.log(`  ${o.path.slice(outdir.length + 1).padEnd(32)} ${(o.size / 1024).toFixed(1)} KB`);
