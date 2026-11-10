"""Public synthetic pages exercise HTTP, JavaScript, selectors and failed requests."""
import http.server
import json
import pathlib
import sys
import tempfile
import threading

from check import check
from pages import run

root = pathlib.Path("/work")
if len(sys.argv) > 1:
    (root / "inputs/pages").mkdir(parents=True, exist_ok=True)
    (root / "inputs/pages/page.html").write_text("<!doctype html><html><body><h1>Orchid inventory</h1><p>Seven pots need watering.</p></body></html>")
    (root / "inputs/pages/pages.json").write_text(json.dumps([
        {"file": "page.html", "name": "heading", "format": "md", "selector": "h1"},
        {"file": "page.html", "name": "article", "format": "txt"}
    ], indent=2) + "\n")
    sys.exit(0)

check(root)
assert "Orchid inventory" in (root / "outputs/heading.md").read_text()
assert "Seven pots need watering." in (root / "outputs/article.txt").read_text()


class Page(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(404 if self.path == "/missing" else 200)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.end_headers()
        self.wfile.write(b"<html><body><h1>Orchid inventory</h1><p id='result'>3 pots</p><script>document.getElementById('result').textContent='7 pots';document.body.dataset.ready='yes'</script></body></html>")

    def log_message(self, *args):
        pass


server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Page)
threading.Thread(target=server.serve_forever, daemon=True).start()
try:
    with tempfile.TemporaryDirectory() as d:
        work = pathlib.Path(d)
        (work / "inputs/pages").mkdir(parents=True)
        url = f"http://127.0.0.1:{server.server_port}"
        pages = [{"url": url, "name": "plain", "format": "txt", "selector": "#result"},
                 {"url": url, "name": "rendered", "format": "html", "browser": True, "selector": "#result", "wait_selector": "body[data-ready=yes]"}]
        (work / "inputs/pages/pages.json").write_text(json.dumps(pages))
        run(work)
        check(work)
        assert "3 pots" in (work / "outputs/plain.txt").read_text()
        assert "7 pots" in (work / "outputs/rendered.html").read_text()
        for request in [{"url": url + "/missing"}, {"url": url, "selector": ".does-not-exist"}, {"url": url, "name": "../escape"}]:
            (work / "inputs/pages/pages.json").write_text(json.dumps([request]))
            try:
                run(work)
            except ValueError:
                pass
            else:
                raise AssertionError(f"invalid/failed request accepted: {request}")
        # A changed input must replace the completed manifest and output.
        (work / "inputs/pages/pages.json").write_text(json.dumps([{"url": url, "name": "changed", "selector": "h1"}]))
        run(work)
        check(work)
        assert "Orchid inventory" in (work / "outputs/changed.md").read_text()
finally:
    server.shutdown()
