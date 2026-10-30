// The known sample: RFC 2119 and Wikipedia's Markdown article. Fails unless
// each page has the content those sources are known to contain.
import { readFileSync } from "node:fs";

const rfc = readFileSync("outputs/www.rfc-editor.org-rfc-rfc2119.html.md", "utf8");
const wiki = readFileSync("outputs/en.wikipedia.org-wiki-Markdown.md", "utf8");
if (!rfc.includes("Key words for use in RFCs")) throw new Error("RFC 2119 title missing");
if (!/lightweight markup language|John Gruber/.test(wiki)) throw new Error("Markdown article content missing");
if (wiki.length <= 10000) throw new Error(`Markdown article only ${wiki.length} bytes`);
console.log(`OK sample: RFC 2119 ${rfc.length} bytes, Markdown article ${wiki.length} bytes`);
