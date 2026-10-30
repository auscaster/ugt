// The task: save each URL in inputs/urls.txt as Markdown in outputs/, through
// the stack's own API.
import { writeFileSync } from "node:fs";
import { page, urls } from "./pages.mjs";

for (const url of urls()) {
  const response = await fetch("http://api:3002/v1/scrape", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ url, formats: ["markdown"] }),
  });
  const reply = await response.json();
  if (!reply.success || !reply.data?.markdown) {
    console.error(`scrape failed for ${url}: ${JSON.stringify(reply).slice(0, 500)}`);
    process.exit(1);
  }
  writeFileSync(page(url), reply.data.markdown);
  console.log(`saved ${page(url)}`);
}
