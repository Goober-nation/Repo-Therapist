// Copies the Russian grading docs (docs/docs-ru/grading) into src/rules so the rules page can
// bundle them. The Docker build context is frontend/ only, so outside the repo checkout the
// source is missing and the committed copy in src/rules is used as is.
import { copyFileSync, existsSync, readdirSync } from "node:fs"
import { dirname, join } from "node:path"
import { fileURLToPath } from "node:url"

const root = dirname(dirname(fileURLToPath(import.meta.url)))
const source = join(root, "..", "docs", "docs-ru", "grading")
const target = join(root, "src", "rules")
const SKIP = new Set(["frontend-results.md"])

if (!existsSync(source)) {
  console.log(`sync-rules: ${source} not found, keeping src/rules as is`)
} else {
  const files = readdirSync(source).filter((name) => name.endsWith(".md") && !SKIP.has(name))
  for (const name of files) copyFileSync(join(source, name), join(target, name))
  console.log(`sync-rules: copied ${files.length} files`)
}
