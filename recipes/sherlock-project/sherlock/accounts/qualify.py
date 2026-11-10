import http.server
import json
import pathlib
import sys
import tempfile
import threading
import uuid

from accounts import check, run

root = pathlib.Path("/work")
if len(sys.argv) > 1:
    (root / "inputs/search.json").write_text(json.dumps({"usernames": ["ugt-proof-" + uuid.uuid4().hex[:20]], "sites": ["GitHub"], "timeout": 15}))
    sys.exit(0)

check(root)


class Sites(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        status = 503 if self.path.startswith("/broken/") else (200 if self.path.endswith("/present") else 404)
        self.send_response(status)
        self.end_headers()
        self.wfile.write(b"A synthetic profile" if status == 200 else b"No synthetic profile")

    def log_message(self, *args):
        pass


server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Sites)
threading.Thread(target=server.serve_forever, daemon=True).start()
try:
    with tempfile.TemporaryDirectory() as d:
        work = pathlib.Path(d)
        (work / "inputs").mkdir()
        url = f"http://127.0.0.1:{server.server_port}"
        definitions = {name: {"urlMain": url, "url": url + prefix + "/{}", "errorType": "status_code", "errorCode": 404, "username_claimed": "present"}
                       for name, prefix in [("Synthetic", ""), ("Unavailable", "/broken")]}
        (work / "inputs/sites.json").write_text(json.dumps(definitions))
        request = {"usernames": ["present", "missing"], "sites": list(definitions), "timeout": 2}
        (work / "inputs/search.json").write_text(json.dumps(request))
        run(work)
        check(work)
        rows = json.loads((work / "outputs/accounts.json").read_text())["rows"]
        statuses = {(r["username"], r["site"]): r["result"] for r in rows}
        assert statuses[("present", "Synthetic")] == "reported_found"
        assert statuses[("missing", "Synthetic")] == "reported_not_found"
        assert all(statuses[(n, "Unavailable")] == "unavailable" for n in request["usernames"])
        request["usernames"] = ["another"]
        (work / "inputs/search.json").write_text(json.dumps(request))
        run(work)
        check(work)
        request["sites"] = ["Unavailable"]
        (work / "inputs/search.json").write_text(json.dumps(request))
        try:
            run(work)
        except ValueError:
            pass
        else:
            raise AssertionError("An entirely failed search was accepted")
        request["usernames"] = ["../outside"]
        (work / "inputs/search.json").write_text(json.dumps(request))
        try:
            run(work)
        except ValueError:
            pass
        else:
            raise AssertionError("A path-like username was accepted")
finally:
    server.shutdown()
