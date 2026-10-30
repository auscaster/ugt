// Fails unless every URL in inputs/urls.txt has a Markdown page in outputs/ with
// real content: at least 100 bytes of text that is not HTML. Works for any URLs;
// the known sample's content is checked separately.
import { existsSync, readFileSync } from "node:fs";
import { page, urls } from "../pages.mjs";

const list = urls();
if (list.length === 0) throw new Error("inputs/urls.txt has no URLs");
for (const url of list) {
  const file = page(url);
  if (!existsSync(file)) throw new Error(`no page for ${url}`);
  const text = readFileSync(file, "utf8");
  if (text.length < 100) throw new Error(`${file} is only ${text.length} bytes`);
  if (!/[A-Za-z]/.test(text)) throw new Error(`${file} has no text`);
  if (/<(html|body|div)\b/i.test(text)) throw new Error(`${file} is HTML, not Markdown`);
  console.log(`ok ${file} (${text.length} bytes)`);
}
console.log(`OK ${list.length} pages`);
