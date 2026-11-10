"""Bounded diagnosis of upstream semantic precision on a real React bundle."""
import json
from pathlib import Path
import time

from analysis import MCP


profiles = [
    {"expression_depth": 8, "getter_expansions": 32, "alias_selections": 32, "primitive_candidates": 32, "primitive_bytes": 65536},
    {"expression_depth": 4, "getter_expansions": 8, "alias_selections": 8, "primitive_candidates": 16, "primitive_bytes": 32768},
    {"expression_depth": 2, "getter_expansions": 1, "alias_selections": 1, "primitive_candidates": 4, "primitive_bytes": 4096},
]
results = []
for precision in profiles:
    client = MCP()
    began = time.monotonic()
    try:
        value = client.tool("analyze_javascript_application", {"input_path": "/samples/react-app", "detail": "summary", "analysis_timeout_ms": 30000, "semantic_precision": precision})
        stats = value["normalized_result"]["summary"]["statistics"]
        parent = value["normalized_result"]["parent_evidence_id"]
        path = Path("/probe") / f"precision-{len(results)}.json"
        client.tool("export_evidence_bundle", {"path": str(path)})
        bundle = json.loads(path.read_text())
        record = next(r for r in bundle["records"] if r["evidence_id"] == parent)
        graph = record["normalized_result"]
        row = {"precision": precision, "seconds": time.monotonic() - began, "statistics": stats, "summary": value, "parsed": stats["parsed_javascript_files"] > 0, "graph_keys": list(graph)}
    except Exception as error:
        row = {"precision": precision, "seconds": time.monotonic() - began, "parsed": False, "error": str(error)}
    finally:
        client.close()
    results.append(row)
    print(json.dumps(row), flush=True)
    Path("/probe/results.json").write_text(json.dumps(results, indent=2) + "\n")
    if row["parsed"]:
        break
raise SystemExit(0 if any(r["parsed"] for r in results) else 1)
