// UGT's page for an "Open in UGT" link: what the recipe will run and reach,
// the files it takes, then its progress and what it gave. Every text from a
// recipe is set as text, never as markup.
"use strict";

const page = document.getElementById("page");

function el(tag, props, ...kids) {
  const e = document.createElement(tag);
  for (const [k, v] of Object.entries(props || {})) {
    if (k === "class") e.className = v;
    else if (k.startsWith("on")) e.addEventListener(k.slice(2), v);
    else if (v === true) e.setAttribute(k, "");
    else if (v !== undefined && v !== null && v !== false) e.setAttribute(k, v);
  }
  for (const kid of kids.flat(Infinity)) {
    if (kid !== undefined && kid !== null && kid !== false) e.append(kid);
  }
  return e;
}

function show(top, ...kids) {
  page.replaceChildren(...kids.flat(Infinity).filter((k) => k !== null && k !== undefined && k !== false));
  if (top) window.scrollTo(0, 0);
}

async function api(path, init) {
  const r = await fetch("api/" + path, init);
  const body = r.status === 204 ? {} : await r.json();
  if (!r.ok) throw new Error(body.error || r.statusText);
  return body;
}

const sentence = (s) => (s ? s.charAt(0).toUpperCase() + s.slice(1) : "");
const base = (p) => p.split("/").pop();
const project = (r) => r.recipe.split("/").slice(0, 2).join("/");
let currentPlan;
let previousForm;
let folder = ""; // the UGT folder, where results go

// inUGT is a path as the user knows it: in their UGT folder
const inUGT = (p) => (folder && p && p.startsWith(folder) ? "UGT" + p.slice(folder.length) : p);

const fileManager = /Mac/.test(navigator.platform) ? "Show in Finder" : /Win/.test(navigator.platform) ? "Show in File Explorer" : "Open the folder";

function seconds(s) {
  s = Math.round(s);
  return s < 60 ? s + " s" : Math.floor(s / 60) + " min " + (s % 60) + " s";
}

function heading(r, title) {
  return [
    el("p", { class: "eyebrow" }, el("a", { href: "https://github.com/" + project(r), target: "_blank", rel: "noreferrer" }, "github.com/" + project(r))),
    el("h1", {}, title),
  ];
}

function list(items) {
  return el("ul", { class: "list" }, items.map(([what, why]) => el("li", {}, what, why ? el("span", { class: "why" }, why) : null)));
}

// before anything runs

function choose(plan) {
  show(true,
    el("p", { class: "eyebrow" }, "github.com/" + project(plan.recipes[0])),
    el("h1", {}, "What do you want it to do?"),
    el("div", { class: "choices" }, plan.recipes.map((r) =>
      el("button", { type: "button", class: "choice", onclick: () => ready(plan, r) },
        el("strong", {}, sentence(r.what)), el("span", {}, r.recipe)))));
}

function inputField(f) {
  const id = "input-" + f.path;
  const file = el("input", { type: "file", name: "file:" + f.path, id, required: true });
  const text = el("textarea", { name: "text:" + f.path, hidden: true, disabled: true, "aria-label": f.what });
  const swap = el("button", { type: "button", class: "swap" }, "Or type it instead");
  swap.addEventListener("click", () => {
    const typing = text.hidden;
    text.hidden = !typing;
    text.required = typing;
    file.hidden = typing;
    file.disabled = typing;
    text.disabled = !typing;
    file.required = !typing;
    swap.textContent = typing ? "Choose a file instead" : "Or type it instead";
    (typing ? text : file).focus();
  });
  return el("div", { class: "field" },
    el("label", { for: id }, sentence(f.what)),
    el("span", { class: "hint" }, base(f.path)),
    el("div", { class: "pick" }, file, /\.(txt|md|csv|json|html?|xml|ya?ml|srt|vtt|tsv)$/i.test(f.path) ? swap : null),
    text);
}

function valueField(s) {
  const id = "value-" + s.name;
  return el("div", { class: "field" },
    el("label", { for: id }, sentence(s.what || s.name)),
    s.how ? el("span", { class: "hint" }, s.how) : null,
    el("input", { type: "text", id, name: "value:" + s.name, required: s.required, autocomplete: "off" }));
}

function gives(r, plan) {
  const items = [];
  for (const o of r.outputs || []) items.push([sentence(o.what), "In your folder " + inUGT(r.results)]);
  const p = r.provides || {};
  for (const x of p.pages || []) items.push([sentence(x.what), "A page to open in your browser"]);
  for (const x of p.endpoints || []) items.push([sentence(x.what), "An address for other software on this computer"]);
  for (const x of p.commands || []) items.push([x.name, sentence(x.what) + ". A command to type in your terminal"]);
  for (const x of p.mcp_servers || []) items.push([sentence(x.what), "Tools for your AI app"]);
  if (!items.length) items.push(["Its own check, run on what it made", ""]);
  return list(items);
}

function reaches(r) {
  if (!(r.access || []).length) {
    return list([["Only what you give it here", "It runs in UGT's sandbox, away from the rest of this computer"]]);
  }
  return list(r.access.map((a) => [a.what, a.why ? "The recipe says: " + a.why : ""]));
}

function returnLink(r, job) {
  return el("a", {href: "https://usegithubthing.com/#project=" + encodeURIComponent(project(r)) + (job ? "&run=" + encodeURIComponent(job) : "")}, job ? "Get help with this run" : "Return to this project on the UGT website");
}
function setupHelp(r, expanded = false) {
  const message = currentPlan.windows_setup
    ? "UGT needs a Linux environment on this computer. Windows may ask for administrator permission and a restart. After restarting, return to your project and finish setup here."
    : "UGT will check what’s installed and set up or repair the environment that runs this project. The first setup may download files.";
  const button = el("button", {type: "button", class: "secondary"}, "Continue with setup");
  const error = el("p", {class: "problem", role: "status", hidden: true});
  button.addEventListener("click", async () => {
    button.disabled = true;
    try {
      const {job} = await api("setup", {method: "POST", body: new URLSearchParams({recipe: r.recipe, agree: "yes"})});
      history.replaceState(null, "", "#job=" + encodeURIComponent(job));
      follow(r, job, true);
    } catch(e) { error.textContent = e.message; error.hidden = false; button.disabled = false; }
  });
  return el("details", {open: expanded}, el("summary", {}, "Need to finish setting up UGT?"), el("p", {class: "note"}, message), button, error);
}

function ready(plan, r) {
  const form = el("form", {});
  form.addEventListener("submit", (e) => {
    e.preventDefault();
    start(r, form);
  });
  if ((r.inputs || []).length) {
    form.append(el("section", {}, el("h2", {}, "What it needs"), r.inputs.map(inputField)));
  }
  const values = (r.settings || []).filter((s) => s.kind === "value");
  const secrets = (r.settings || []).filter((s) => s.kind === "secret");
  const required = values.filter(s => s.required), optional = values.filter(s => !s.required);
  if (optional.length) form.append(el("details", {}, el("summary", {}, "Change the defaults"), optional.map(valueField)));
  if (required.length || secrets.length) {
    form.append(el("section", {}, el("h2", {}, "What it needs from you"), required.map(valueField),
      secrets.length ? el("p", { class: "note field" }, "UGT asks for " + secrets.map((s) => s.what || s.name).join(" and ") +
        " in a separate window. Enter it there instead of in chat.") : null));
  }
  const limits = [...(r.known_issues || [])];
  if (r.installation_limit) limits.push(sentence(r.installation_limit));
  if (limits.length) {
    form.append(el("section", {}, el("h2", {}, "Before you run it"), list(limits.map((issue) => [issue, ""]))));
  }
  if (r.kind === "service" && !r.installation_limit) {
    form.append(el("details", {}, el("summary", {}, "Run a separate copy"),
      el("div", { class: "field" }, el("label", { for: "instance" }, "Copy name"),
        el("span", { class: "hint" }, "A new name starts with its own data and settings. Use the same name to reopen it."),
        el("input", { type: "text", id: "instance", name: "instance", maxlength: 32, pattern: "[a-z0-9]+(-[a-z0-9]+)*", placeholder: "e.g. test", autocomplete: "off" }))));
  }
  form.append(
    el("section", {}, el("h2", {}, "What it gives you"), gives(r, plan)),
    el("details", {}, el("summary", {}, "What this project can access"), reaches(r)),
    el("div", { class: "actions" },
      el("button", { type: "submit", class: "primary" }, "Run"),
      el("span", { class: "note" }, "It runs in UGT's private Linux engine, on this computer.")));
  previousForm = {r, form};
  showForm(r, form);
}
function showForm(r, form) {
  const button = form.querySelector(".primary");
  button.disabled = false; button.textContent = r.kind === "service" ? "Start the app" : "Run";
  show(true, ...heading(r, sentence(r.what)), form, setupHelp(r), currentPlan.recipes.length > 1
    ? el("button", {type: "button", class: "secondary", onclick: () => choose(currentPlan)}, "Choose another task") : null);
}
function another(r, instance, job) {
  history.replaceState(null, "", location.pathname + (job ? "#again=" + encodeURIComponent(job) : ""));
  if (previousForm?.r === r) showForm(r, previousForm.form);
  else {
    ready(currentPlan, r);
    const name = previousForm.form.elements.namedItem("instance");
    if (name && instance) { name.value = instance.split("@")[1] || ""; name.closest("details").open = !!name.value; }
  }
}

async function start(r, form) {
  const button = form.querySelector(".primary");
  button.disabled = true;
  button.textContent = "Starting";
  const data = new FormData(form);
  data.append("recipe", r.recipe);
  try {
    const { job } = await api("run", { method: "POST", body: data });
    history.replaceState(null, "", "#job=" + encodeURIComponent(job));
    follow(r, job, true);
  } catch (e) {
    if (e instanceof TypeError) {
      show(true, ...heading(r, "Let’s check whether it started"), el("p", {}, "The connection dropped before UGT could confirm. Refresh this page to look for the run."), retryPage());
      return;
    }
    button.disabled = false;
    button.textContent = r.kind === "service" ? "Start the app" : "Run";
    form.querySelector(".problem")?.remove();
    form.append(el("p", { class: "problem" }, sentence(e.message)));
  }
}

// while it runs, and after

function retryPage() {
  return el("button", {type: "button", class: "secondary", onclick: () => location.reload()}, "Refresh this page");
}
async function follow(r, id, first) {
  let j;
  try {
    j = await api("job?id=" + encodeURIComponent(id));
  } catch (e) {
    show(first, el("h1", {}, "We lost touch with UGT"), el("p", { class: "problem" }, "Refresh to check the same run. If UGT has closed, reopen your project from the website."), retryPage());
    return;
  }
  r = currentPlan.recipes.find(candidate => candidate.recipe === j.recipe);
  if (!r) { show(true, el("h1", {}, "This run belongs to another project.")); return; }
  if (j.state === "queued" || j.state === "running") {
    if (j.kind === "setup") show(first, ...heading(r, "Setting up UGT"), steps(j.events || [], true));
    else running(r, j, first);
    setTimeout(() => follow(r, id, false), 1200);
    return;
  }
  if (j.kind === "setup") {
    const res = j.result || {};
    if (res.ready && j.state === "succeeded") {
      show(true, ...heading(r, "UGT is ready"), el("button", {type: "button", class: "primary", onclick: () => another(r)}, "Continue with my project"));
    } else if (res.needs_restart) {
      show(true, ...heading(r, "Restart Windows to continue"), el("p", {}, "After you sign back in, open this project again and choose Continue with setup. Your project is saved on the website if you started there."), returnLink(r));
    } else {
      show(true, ...heading(r, "Setup needs attention"), el("p", {class: "problem"}, j.error || (res.next || []).join(" ") || "UGT could not finish setup on this computer."), setupHelp(r, true), returnLink(r));
    }
    return;
  }
  finished(r, j);
}

function steps(events, live) {
  return el("ol", { class: "steps" }, events.map((e, i) =>
    el("li", { class: live && i === events.length - 1 ? "now" : "" },
      e.step ? el("span", { class: "step" }, e.step) : null, sentence(e.message))));
}

function running(r, j, first) {
  const since = (Date.now() - Date.parse(j.started)) / 1000;
  show(first, ...heading(r, sentence(r.what)),
    el("p", { class: "status" }, "Running for " + seconds(since)),
    steps(j.events || [], true));
}

function finished(r, j) {
  const res = j.result || {};
  if (j.state !== "succeeded" || !res.passed) {
    show(true, ...heading(r, "It did not finish"),
      el("p", { class: "problem" }, sentence(j.error) || "Its check did not pass on what it made."),
      el("div", { class: "actions" }, el("button", {type: "button", class: "primary", onclick: () => another(r, j.result?.installation, j.id)}, r.kind === "service" ? "Review setup and try again" : "Change inputs and try again")),
      setupHelp(r), el("details", {}, el("summary", {}, "Show run details"), steps(j.events || [], false)), returnLink(r, j.id));
    return;
  }
  const parts = [...heading(r, res.files?.length ? (res.files.length === 1 ? "Your file is ready" : "Your files are ready") : res.kind === "service" ? "Your app started" : "Finished"), el("p", { class: "status" }, sentence(r.what) + ", in " + seconds(res.seconds || 0))];
  if (res.installation) parts.push(el("p", { class: "note" }, res.installation));
  if (res.results) {
    const reveal = el("button", { type: "button", class: "secondary" }, fileManager);
    const issue = el("p", {class: "problem", hidden: true, role: "status"});
    reveal.addEventListener("click", async () => {
      try { await api("reveal?id=" + encodeURIComponent(j.id), { method: "POST" }); issue.hidden = true; }
      catch { issue.textContent = "Open this folder yourself: " + res.results; issue.hidden = false; }
    });
    parts.push(el("section", {}, el("h2", {}, "Your files"),
      list((res.files || []).map((f) => [f, ""])),
      el("div", { class: "actions" }, reveal, el("span", { class: "note" }, "In your folder " + inUGT(res.results)), issue)));
  }
  if ((res.pages || []).length) {
    parts.push(el("section", {}, el("h2", {}, "Open"), el("div", { class: "actions" },
      res.pages.map((p) => el("a", { class: "secondary", href: p.url, target: "_blank", rel: "noreferrer" }, sentence(p.what))))));
  }
  if ((res.endpoints || []).length) {
    parts.push(el("section", {}, el("h2", {}, "For other software"), list(res.endpoints.map((x) => [x.url, sentence(x.what)]))));
  }
  if ((res.commands || []).length) {
    parts.push(el("section", {}, el("h2", {}, "Type in your terminal"),
      res.commands.map((c) => [el("pre", {}, el("code", {}, c.type)), el("p", { class: "note" }, sentence(c.what) + (c.note ? ". " + c.note : ""))])));
  }
  if ((res.mcp_servers || []).length) {
    parts.push(el("section", {}, el("h2", {}, "Ask your AI app"), list(res.mcp_servers.map((m) => [sentence(m.what), m.note]))));
  }
  if (res.kind === "service") {
    const stop = el("button", {type: "button", class: "secondary"}, "Stop this copy");
    const status = el("p", {class: "note", role: "status"}, "Stopping the app keeps its data on this computer.");
    stop.addEventListener("click", async () => {
      stop.disabled = true;
      try { await api("stop?id=" + encodeURIComponent(j.id), {method: "POST"});
        history.replaceState(null, "", location.pathname + "#again=" + encodeURIComponent(j.id));
        stopped(r, res.installation, j.id);
      } catch (e) { status.textContent = "UGT couldn’t stop the app: " + e.message; stop.disabled = false; }
    });
    parts.push(el("div", {class: "actions"}, stop, status));
  } else parts.push(el("div", {class: "actions"}, el("button", {type: "button", class: "secondary", onclick: () => another(r, undefined, j.id)}, "Run again")));
  parts.push(el("p", {class: "note"}, returnLink(r)));
  show(true, el("div", { class: "done" }, parts));
}
function stopped(r, instance, job) {
  show(true, ...heading(r, "You stopped this copy"), el("p", {class: "note"}, "Your data stays on this computer. To use it again, start the same copy."),
    el("div", {class: "actions"}, el("button", {type: "button", class: "primary", onclick: () => another(r, instance, job)}, "Back to the app")));
}

async function main() {
  setInterval(() => fetch("api/alive").catch(() => {}), 15000);
  let plan;
  try {
    plan = await api("plan");
  } catch (e) {
    show(true, el("h1", {}, "We lost touch with UGT"), el("p", { class: "problem" }, "Refresh this page. If UGT has closed, reopen your project from the website."), retryPage());
    return;
  }
  currentPlan = plan; folder = plan.folder;
  if (plan.error) {
    show(true, el("h1", {}, "UGT cannot open this link"), el("p", { class: "problem" }, sentence(plan.error)), retryPage());
    return;
  }
  const route = new URLSearchParams(location.hash.slice(1));
  const job = route.get("job") || route.get("again") || plan.last_job;
  if (job) {
    if (route.get("again")) {
      try { const j = await api("job?id=" + encodeURIComponent(job)); const r = plan.recipes.find(r => r.recipe === j.recipe);
        if (r) { another(r, j.result?.installation, j.id); return; }
      } catch { /* follow will offer recovery */ }
    }
    await follow(null, job, true); return;
  }
  if (plan.recipes.length === 1) ready(plan, plan.recipes[0]);
  else choose(plan);
}

main();
