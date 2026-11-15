// Writes a .gz sibling for every compressible file in the build output and
// restores the placeholder that keeps the directory in version control.
import { readdirSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { gzipSync, constants } from "node:zlib";

const out = new URL("../../internal/webui/dist/", import.meta.url).pathname;
const exts = [".js", ".css", ".html", ".svg", ".json", ".txt", ".map"];

function walk(dir) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) {
      walk(p);
    } else if (exts.some((e) => name.endsWith(e))) {
      const raw = readFileSync(p);
      const gz = gzipSync(raw, { level: constants.Z_BEST_COMPRESSION });
      if (gz.length < raw.length) writeFileSync(p + ".gz", gz);
    }
  }
}

walk(decodeURIComponent(out));
writeFileSync(join(decodeURIComponent(out), ".keep"), "");
