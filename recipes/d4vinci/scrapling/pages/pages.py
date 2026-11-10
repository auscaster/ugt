"""Batch the upstream fetchers; leave their CLI/MCP available for other uses."""
import hashlib
import json
import pathlib
import re
import sys
from urllib.parse import urlsplit

from scrapling import Selector
from scrapling.core.shell import Convertor
from scrapling.fetchers import Fetcher, StealthyFetcher


def requests(root):
    pages = json.loads((root / "inputs/pages.json").read_text())
    if not isinstance(pages, list) or not 1 <= len(pages) <= 100:
        raise ValueError("pages.json must contain between 1 and 100 page requests")
    names = set()
    for i, page in enumerate(pages, 1):
        if not isinstance(page, dict) or set(page) - {"url", "html", "name", "format", "selector", "browser", "wait_selector", "timeout"}:
            raise ValueError(f"page {i}: unknown fields or invalid request")
        if ("url" in page) == ("html" in page):
            raise ValueError(f"page {i}: supply url or inline html, but not both")
        if "url" in page and (not isinstance(page["url"], str) or urlsplit(page["url"]).scheme not in {"http", "https"} or not urlsplit(page["url"]).hostname):
            raise ValueError(f"page {i}: supply an http or https URL")
        if "html" in page:
            if not isinstance(page["html"], str) or not page["html"].strip():
                raise ValueError(f"page {i}: html must contain nonempty HTML text")
            if page.get("browser"):
                raise ValueError(f"page {i}: inline HTML extraction is static; use a URL for browser mode")
        name, fmt = page.get("name", f"page-{i}"), page.get("format", "md")
        if not isinstance(name, str) or not re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9_-]{0,79}", name) or name in names:
            raise ValueError(f"page {i}: choose a unique filename using letters, numbers, hyphens or underscores")
        if fmt not in {"md", "txt", "html"}:
            raise ValueError(f"page {i}: format must be md, txt or html")
        if type(page.get("browser", False)) is not bool:
            raise ValueError(f"page {i}: browser must be true or false")
        timeout = page.get("timeout", 30)
        if type(timeout) is not int or not 1 <= timeout <= 120:
            raise ValueError(f"page {i}: timeout must be 1–120 seconds")
        for key in ["selector", "wait_selector"]:
            if key in page and (not isinstance(page[key], str) or not page[key].strip()):
                raise ValueError(f"page {i}: {key} must be a nonempty CSS selector")
        if "wait_selector" in page and not page.get("browser"):
            raise ValueError(f"page {i}: wait_selector needs browser: true")
        names.add(name)
        yield page, f"{name}.{fmt}"


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def identity(page):
    request = {k: v for k, v in page.items() if k != "html"}
    if "html" in page:
        request["html_sha256"] = hashlib.sha256(page["html"].encode()).hexdigest()
    return request


def run(root):
    pages = list(requests(root))
    out = root / "outputs"
    out.mkdir(exist_ok=True)
    (out / "pages.json").unlink(missing_ok=True)
    (out / "sources").mkdir(exist_ok=True)
    records = []
    for page, filename in pages:
        label = page.get("url", "inline HTML")
        if "html" in page:
            response = Selector(page["html"])
            status = None
        elif page.get("browser"):
            response = StealthyFetcher.fetch(page["url"], headless=True, timeout=page.get("timeout", 30) * 1000,
                                            wait_selector=page.get("wait_selector"),
                                            additional_args={"ignore_https_errors": False})
            status = response.status
        else:
            response = Fetcher.get(page["url"], timeout=page.get("timeout", 30))
            status = response.status
        if status is not None and not 200 <= status < 300:
            raise ValueError(f"{label}: HTTP {status}; no completed result was saved")
        if page.get("selector") and not response.css(page["selector"]):
            raise ValueError(f"{label}: selector {page['selector']!r} matched nothing")
        source = out / "sources" / (filename + ".html")
        source.write_text(response.html_content, encoding="utf-8")
        target = out / filename
        Convertor.write_content_to_file(response, str(target), page.get("selector"))
        if not target.read_text().strip():
            raise ValueError(f"{label}: selected content was empty")
        records.append({"request": identity(page), "file": filename, "status": status,
                        "source_sha256": digest(source), "sha256": digest(target)})
    (out / "pages.json").write_text(json.dumps(records, indent=2) + "\n")


if __name__ == "__main__":
    try:
        run(pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else "/work"))
    except Exception as error:
        sys.exit(f"Could not save the pages: {error}")
