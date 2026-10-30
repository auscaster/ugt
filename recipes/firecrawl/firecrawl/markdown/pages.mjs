// Shared by scrape.mjs and the checks: the URLs to save, and the Markdown file
// each one is saved as. Paths are relative to /work.
import { readFileSync } from "node:fs";

export const urls = () =>
  readFileSync("inputs/urls.txt", "utf8")
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line && !line.startsWith("#"));

export const page = (url) =>
  "outputs/" + url.replace(/^[a-z]+:\/\//, "").replace(/[^A-Za-z0-9._-]+/g, "-").replace(/-+$/, "") + ".md";
