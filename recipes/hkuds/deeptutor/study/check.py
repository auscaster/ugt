"""Read, annotate and ask about an actual uploaded document through the app."""
import asyncio
import json
import sys
import uuid

import httpx
import websockets

base = "http://deeptutor:3782"
sample = len(sys.argv) > 1
marker = uuid.uuid4().hex[:12]
subject = "violet" if sample else "orange"
passage = f"# Greenhouse notes {marker}\n\nThe greenhouse door is painted {subject}. The garden has seven pots.\n"
material = session = None


def api(method, path, **kwargs):
    response = httpx.request(method, base + path, timeout=45, **kwargs)
    response.raise_for_status()
    return response.json() if response.content else None


async def ask():
    global session
    text = []
    saved_session = None
    async with asyncio.timeout(240):
        async with websockets.connect("ws://deeptutor:3782/ws", open_timeout=20, max_size=8 * 1024 * 1024) as ws:
            await ws.send(json.dumps({
                "type": "start_turn", "protocol_version": "2.0", "capability": "chat",
                "content": "From the supplied reading passage, what colour is the greenhouse door? Answer with the colour only.",
                "reading_references": [{"material_id": material["material_id"], "revision": material["revision"], "locators": [1]}],
                "config": {"auto_route": False}, "auto_route": False,
                "llm_selection": {"profile_id": "local-ollama", "model_id": "qwen-local"}
            }))
            while True:
                event = json.loads(await ws.recv())
                if event.get("session_id"):
                    saved_session = event["session_id"]
                    session = saved_session
                if event["type"] in {"error", "protocol_error"}:
                    raise AssertionError(event)
                if event["type"] in {"content", "result"}:
                    text.append(event.get("content", ""))
                if event["type"] == "done":
                    break
    answer = " ".join(text).lower()
    assert subject in answer, f"Tutor did not answer from the uploaded passage: {answer}"
    return saved_session


try:
    # The same frontend origin the user's browser uses must serve both the app and API.
    response = httpx.get(base + "/reading", timeout=15, follow_redirects=True)
    response.raise_for_status()
    assert "<!doctype html" in response.text.lower()
    status = api("GET", "/api/auth/status")
    assert status["authenticated"], "The local reading API is not available"
    material = api("POST", "/api/reading/materials", files={"file": (f"ugt-check-{marker}.md", passage.encode(), "text/markdown")})
    assert material["unit_count"] >= 1 and material["char_count"] > 0
    unit = api("GET", f"/api/reading/materials/{material['material_id']}/units/1")
    assert subject in unit["text"] and marker in unit["text"]
    api("PUT", f"/api/reading/materials/{material['material_id']}/position", json={"locator": 1, "percentage": 0.5})
    # An independent request must find the persisted document and position.
    reopened = api("GET", f"/api/reading/materials/{material['material_id']}")
    assert reopened["material_id"] == material["material_id"]
    position = api("GET", f"/api/reading/materials/{material['material_id']}/position")
    assert position["locator"] == 1 and position["percentage"] == 0.5
    session = asyncio.run(ask())
    assert session, "Tutor did not save its conversation"
    conversation = api("GET", f"/api/sessions/{session}")
    assert subject in json.dumps(conversation).lower(), "Saved conversation lost the answer"
    missing = httpx.get(base + "/api/reading/materials/ugt-missing-" + marker, timeout=10)
    assert missing.status_code == 404, "A missing document was accepted"
    print("Uploaded, read, reopened and answered from a document with the local model.")
finally:
    # Delete only this check's own synthetic records. Never touch the user's material.
    if session:
        api("DELETE", f"/api/sessions/{session}")
    if material:
        api("DELETE", f"/api/reading/materials/{material['material_id']}")
