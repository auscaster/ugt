"""Check that Paperless consumes a new document and indexes its searchable text."""
from pathlib import Path
import sqlite3
import time
from uuid import uuid4
from page import check

check()
name = "Harbour invoice " + uuid4().hex[:12]
text = "The harbour invoice includes sixty brass lanterns."
inbox = Path("/usr/src/paperless/consume")
tmp = inbox / (name + ".part")
tmp.write_text(text)
tmp.rename(inbox / (name + ".txt"))
deadline = time.monotonic() + 150
with sqlite3.connect("file:/usr/src/paperless/data/db.sqlite3?mode=ro", uri=True) as db:
    while time.monotonic() < deadline:
        row = db.execute("SELECT content FROM documents_document WHERE title = ?", (name,)).fetchone()
        if row and "sixty brass lanterns" in row[0].casefold():
            break
        time.sleep(2)
    else:
        raise SystemExit("The scanner inbox did not produce a searchable document")
