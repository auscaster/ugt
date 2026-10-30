// Fails unless every URL in inputs/urls.txt has a Markdown page in outputs/
// with real content: valid UTF-8 text of at least 100 bytes, with at least one
// Markdown heading or link, and not HTML. Works for any URLs; the known
// sample's content is checked separately.
import { existsSync, readFileSync } from "node:fs";
import { page, urls } from "../pages.mjs";

const utf8 = new TextDecoder("utf-8", { fatal: true });
const list = urls();
if (list.length === 0) throw new Error("inputs/urls.txt has no URLs");
for (const url of list) {
  const file = page(url);
  if (!existsSync(file)) throw new Error(`no page for ${url}`);
  const bytes = readFileSync(file);
  if (bytes.length < 100) throw new Error(`${file} is only ${bytes.length} bytes`);
  let text;
  try {
    text = utf8.decode(bytes);
  } catch {
    throw new Error(`${file} is not UTF-8 text`);
  }
  if (!/^#{1,6} |\]\(/m.test(text)) throw new Error(`${file} has no Markdown heading or link`);
  if (/<(html|body|div)\b/i.test(text)) throw new Error(`${file} is HTML, not Markdown`);
  console.log(`ok ${file} (${bytes.length} bytes)`);
}
console.log(`OK ${list.length} pages`);
