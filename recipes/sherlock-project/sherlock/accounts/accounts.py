import copy
import csv
import hashlib
import json
import pathlib
import re
import sys

from sherlock_project.notify import QueryNotify
from sherlock_project.sherlock import sherlock
from sherlock_project.sites import SitesInformation
import sherlock_project

FIELDS = ["username", "site", "url", "result", "upstream_result", "http_status"]


def configuration(root):
    request = json.loads((root / "inputs/search.json").read_text())
    if not isinstance(request, dict) or set(request) - {"usernames", "sites", "timeout", "nsfw"}:
        raise ValueError("search.json must contain usernames, sites and optional timeout/nsfw")
    names, sites = request.get("usernames"), request.get("sites")
    if not isinstance(names, list) or not 1 <= len(names) <= 20 or any(not isinstance(n, str) for n in names):
        raise ValueError("supply 1–20 distinct usernames")
    if len(set(names)) != len(names):
        raise ValueError("supply distinct usernames")
    if any(not isinstance(n, str) or not re.fullmatch(r"[\w][\w.-]{0,127}", n) for n in names):
        raise ValueError("usernames must start with a letter, number or underscore, using letters, numbers, underscores, dots or hyphens")
    if not isinstance(sites, list) or not sites or any(not isinstance(s, str) or not s for s in sites) or len(set(s.casefold() for s in sites)) != len(sites):
        raise ValueError("choose distinct site names in sites")
    if type(request.get("timeout", 10)) is not int or not 1 <= request.get("timeout", 10) <= 60:
        raise ValueError("timeout must be 1–60 seconds")
    if type(request.get("nsfw", False)) is not bool:
        raise ValueError("nsfw must be true or false")
    return request


def fingerprint(root):
    digest = hashlib.sha256((root / "inputs/search.json").read_bytes())
    custom = root / "inputs/sites.json"
    if custom.exists():
        digest.update(custom.read_bytes())
    return digest.hexdigest()


def csv_row(row):
    # Keep raw values in JSON; prevent a custom site name becoming an Excel formula.
    values = {k: "" if row[k] is None else str(row[k]) for k in FIELDS}
    return {k: "'" + v if v.lstrip().startswith(("=", "+", "-", "@")) else v for k, v in values.items()}


def run(root):
    request = configuration(root)
    out = root / "outputs"
    out.mkdir(exist_ok=True)
    (out / "accounts.json").unlink(missing_ok=True)
    custom = root / "inputs/sites.json"
    source = custom if custom.exists() else pathlib.Path(sherlock_project.__file__).parent / "resources/data.json"
    definitions = SitesInformation(str(source), honor_exclusions=not custom.exists())
    if not request.get("nsfw"):
        definitions.remove_nsfw_sites()
    available = {site.name.casefold(): site for site in definitions}
    missing = [s for s in request["sites"] if s.casefold() not in available]
    if missing:
        raise ValueError(f"Unknown, excluded or disabled sites: {', '.join(missing)}")
    selected = {available[s.casefold()].name: available[s.casefold()].information for s in request["sites"]}
    rows = []
    for username in request["usernames"]:
        result = sherlock(username, copy.deepcopy(selected), QueryNotify(), timeout=request.get("timeout", 10))
        for site, entry in result.items():
            code = int(entry["http_status"]) if entry.get("http_status") else None
            upstream = str(entry["status"].status)
            if code is None or code in {403, 429} or code >= 500:
                status = "unavailable"
            else:
                status = {"Claimed": "reported_found", "Available": "reported_not_found", "Illegal": "invalid_username"}.get(upstream, "unavailable")
            rows.append(dict(username=username, site=site, url=entry["url_user"], result=status, upstream_result=upstream, http_status=code))
    if not any(r["result"] in {"reported_found", "reported_not_found"} for r in rows):
        raise ValueError("No selected site could be checked; blocked or failed requests are not proof of absence")
    with (out / "accounts.csv").open("w", newline="") as file:
        writer = csv.DictWriter(file, fieldnames=FIELDS)
        writer.writeheader()
        writer.writerows(csv_row(row) for row in rows)
    (out / "accounts.json").write_text(json.dumps({"input_sha256": fingerprint(root), "rows": rows}, indent=2) + "\n")


def check(root):
    request = configuration(root)
    report = json.loads((root / "outputs/accounts.json").read_text())
    assert report["input_sha256"] == fingerprint(root), "Inputs changed after the search"
    rows = report["rows"]
    expected = {(n, s.casefold()) for n in request["usernames"] for s in request["sites"]}
    assert len(rows) == len(expected) and {(r["username"], r["site"].casefold()) for r in rows} == expected
    for row in rows:
        assert row["url"].startswith(("http://", "https://"))
        assert row["result"] in {"reported_found", "reported_not_found", "invalid_username", "unavailable"}
        if row["http_status"] is None or row["http_status"] in {403, 429} or row["http_status"] >= 500:
            assert row["result"] == "unavailable"
    assert any(r["result"] in {"reported_found", "reported_not_found"} for r in rows)
    with (root / "outputs/accounts.csv").open(newline="") as file:
        reader = csv.DictReader(file)
        assert reader.fieldnames == FIELDS
        assert list(reader) == [csv_row(row) for row in rows]


if __name__ == "__main__":
    try:
        (check if len(sys.argv) > 1 else run)(pathlib.Path("/work"))
    except Exception as error:
        sys.exit(f"Could not check the usernames: {error}")
