import json
import pathlib
import tempfile

from scrapling import Selector
from scrapling.core.shell import Convertor
from pages import digest, requests


def check(root):
    expected = list(requests(root))
    out = root / "outputs"
    records = json.loads((out / "pages.json").read_text())
    assert len(records) == len(expected), "a requested page is missing"
    for (request, filename), record in zip(expected, records):
        assert record["request"] == request and record["file"] == filename
        assert 200 <= record["status"] < 300
        source, target = out / "sources" / (filename + ".html"), out / filename
        assert digest(source) == record["source_sha256"] and digest(target) == record["sha256"]
        assert target.read_text().strip(), "empty extraction"
        # Reconstruct from retained source, rather than trusting a success flag or hash alone.
        page = Selector(source.read_text())
        if request.get("selector"):
            assert page.css(request["selector"]), "selection absent from source"
        with tempfile.TemporaryDirectory() as d:
            rebuilt = pathlib.Path(d) / filename
            Convertor.write_content_to_file(page, str(rebuilt), request.get("selector"))
            assert rebuilt.read_bytes() == target.read_bytes(), "output differs from fetched content"


if __name__ == "__main__":
    check(pathlib.Path("/work"))
