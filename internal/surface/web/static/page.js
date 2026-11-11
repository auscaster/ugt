// The local workflow reads recipe content and follows the actual helper job.
// Submission feedback is UI state; it never stands in for a confirmed run.
"use strict";

const page = document.getElementById("page");
let currentPlan, previousForm, pollTimer;
let followVersion = 0;
let folder = "";

function el(tag, props, ...kids) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(props || {})) {
    if (key === "class") node.className = value;
    else if (key.startsWith("on")) node.addEventListener(key.slice(2), value);
    else if (value === true) node.setAttribute(key, "");
    else if (value !== undefined && value !== null && value !== false) node.setAttribute(key, value);
  }
  for (const child of kids.flat(Infinity)) if (child !== undefined && child !== null && child !== false) node.append(child);
  return node;
}

function icon(name) {
  const paths = { check: "m6 12 4 4 8-8", file: "M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z M14 2v6h6 M8 13h8 M8 17h6", arrow: "M5 12h14 m-6-6 6 6-6 6", alert: "M12 8v5 m0 3v.1 M10.3 3.8 2 18a2 2 0 0 0 1.7 3h16.6a2 2 0 0 0 1.7-3L13.7 3.8a2 2 0 0 0-3.4 0" };
  const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  for (const [key, value] of Object.entries({ viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", "stroke-width": "1.6", "stroke-linecap": "round", "stroke-linejoin": "round", "aria-hidden": "true" })) svg.setAttribute(key, value);
  const path = document.createElementNS(svg.namespaceURI, "path"); path.setAttribute("d", paths[name]); svg.append(path); return svg;
}

function show(top, ...kids) {
  page.removeAttribute("aria-busy");
  page.replaceChildren(...kids.flat(Infinity).filter(k => k !== null && k !== undefined && k !== false));
  if (!matchMedia("(prefers-reduced-motion: reduce)").matches) {
    page.getAnimations().forEach(animation => animation.cancel());
    page.animate([{ opacity: .4, transform: "translateY(5px)" }, { opacity: 1, transform: "translateY(0)" }], { duration: 180, easing: "cubic-bezier(.22,1,.36,1)" });
  }
  if (top) { window.scrollTo(0, 0); page.querySelector("h1")?.focus({ preventScroll: true }); }
}

async function api(path, init) {
  const response = await fetch("api/" + path, init);
  const text = response.status === 204 ? "" : await response.text();
  let body;
  try { body = text ? JSON.parse(text) : {}; }
  catch { if (response.ok) throw new TypeError("UGT did not return a confirmation."); throw new Error("UGT could not accept this request. Try again."); }
  if (!response.ok) throw new Error(body.error || response.statusText);
  return body;
}

const sentence = value => value ? value.charAt(0).toUpperCase() + value.slice(1) : "";
const base = path => path.split("/").pop();
const project = recipe => recipe.recipe.split("/").slice(0, 2).join("/");
const nameOf = recipe => recipe ? recipe.recipe.split("/")[1] : "your project";
const inUGT = path => folder && path && path.startsWith(folder) ? "UGT" + path.slice(folder.length) : path;
const fileManager = /Mac/.test(navigator.platform) ? "Show in Finder" : /Win/.test(navigator.platform) ? "Show in File Explorer" : "Open the folder";
function seconds(value) { const s = Math.max(0, Math.floor(value)); return s < 60 ? s + " s" : Math.floor(s / 60) + " min " + s % 60 + " s"; }
function stopFollowing() { followVersion++; clearTimeout(pollTimer); }
function heading(recipe, title, note, complete = false) {
  return [el("header", { class: "view-heading" },
    recipe ? el("p", { class: "eyebrow" }, el("a", { href: "https://github.com/" + project(recipe), target: "_blank", rel: "noreferrer" }, project(recipe))) : null,
    el("h1", { tabindex: "-1" }, complete ? el("span", { class: "success-mark" }, icon("check")) : null, title),
    note ? el("p", { class: "lede" }, note) : null)];
}
function panel(...kids) { return el("div", { class: "workbench" }, kids); }
function list(items, className = "") {
  return el("ul", { class: "list " + className }, items.map(([what, why]) => el("li", {},
    className === "files" ? el("span", { class: "file-mark" }, icon("file")) : null,
    el("div", { class: "list-copy" }, el("span", { class: "list-title" }, what), why ? el("span", { class: "why" }, why) : null))));
}
function returnLink(recipe, job) {
  return el("a", { class: "back-link", href: "https://usegeekthing.com/#project=" + encodeURIComponent(project(recipe)) + (job ? "&run=" + encodeURIComponent(job) : "") }, job ? "Get help with this run" : "Back to this project on the website");
}
function retryPage() { return el("button", { type: "button", class: "secondary", onclick: () => location.reload() }, "Refresh this page"); }
function problem(text) { return el("p", { class: "problem", role: "alert" }, text); }

function choose(plan) {
  stopFollowing();
  show(true, ...heading(plan.recipes[0], "What would you like to do?"),
    el("div", { class: "choices" }, plan.recipes.map(recipe => el("button", { type: "button", class: "choice", onclick: () => ready(plan, recipe) },
      el("span", {}, el("strong", {}, sentence(recipe.what)), el("span", { class: "note" }, base(recipe.recipe))), icon("arrow")))));
}

function inputField(input) {
  const id = "input-" + input.path, hint = id + "-hint";
  const file = el("input", { type: "file", name: "file:" + input.path, id, required: true, "aria-describedby": hint });
  const text = el("textarea", { name: "text:" + input.path, hidden: true, disabled: true, "aria-label": input.what });
  const info = el("span", { id: hint, class: "hint" }, "UGT uses a copy of what you choose.");
  let mode = "file";
  const textButton = el("button", { type: "button", class: "text-button" }, "Type it instead");
  const directoryButton = el("button", { type: "button", class: "text-button" }, "Choose a folder instead");
  function setMode(next) {
    mode = next; file.value = ""; file.hidden = next === "text"; file.disabled = next === "text"; file.required = next !== "text";
    file.multiple = next === "directory";
    file.toggleAttribute("webkitdirectory", next === "directory");
    file.name = (next === "directory" ? "directory:" : "file:") + input.path;
    text.hidden = next !== "text"; text.disabled = next !== "text"; text.required = next === "text";
    directoryButton.textContent = next === "directory" ? "Choose a file instead" : "Choose a folder instead";
    textButton.textContent = next === "text" ? "Choose a file instead" : "Type it instead";
    info.textContent = next === "directory" ? "Choose the folder itself. UGT copies its files and subfolders." : "UGT uses a copy of what you choose.";
    (next === "text" ? text : file).focus();
  }
  directoryButton.addEventListener("click", () => setMode(mode === "directory" ? "file" : "directory"));
  textButton.addEventListener("click", () => setMode(mode === "text" ? "file" : "text"));
  return el("div", { class: "field" }, el("label", { for: id }, sentence(input.what)), info,
    el("div", { class: "pick" }, file), text,
    el("div", { class: "input-alternatives" }, directoryButton, /\.(txt|md|csv|json|html?|xml|ya?ml|srt|vtt|tsv)$/i.test(input.path) ? textButton : null));
}
function valueField(setting) {
  const id = "value-" + setting.name;
  return el("div", { class: "field" }, el("label", { for: id }, sentence(setting.what || setting.name)),
    setting.how ? el("span", { class: "hint" }, setting.how) : null,
    el("input", { type: "text", id, name: "value:" + setting.name, required: setting.required, autocomplete: "off" }));
}
function gives(recipe) {
  const items = (recipe.outputs || []).map(output => [sentence(output.what), base(output.path)]);
  const provides = recipe.provides || {};
  for (const item of provides.pages || []) items.push([sentence(item.what), "A page to open on this computer"]);
  for (const item of provides.endpoints || []) items.push([sentence(item.what), "An address for other software"]);
  for (const item of provides.commands || []) items.push([item.name, sentence(item.what)]);
  for (const item of provides.mcp_servers || []) items.push([sentence(item.what), "Tools for your AI app"]);
  return list(items.length ? items : [["A result checked by the project", ""]]);
}
function reaches(recipe) {
  return list((recipe.access || []).length ? recipe.access.map(item => [item.what, item.why ? "The recipe says: " + item.why : ""]) : [["Only what you give it here", "It runs in UGT’s sandbox, away from the rest of this computer."]]);
}
function setupHelp(recipe, expanded = false) {
  const button = el("button", { type: "button", class: "secondary" }, "Continue with setup");
  const issue = problem(""); issue.hidden = true;
  button.addEventListener("click", async () => {
    button.disabled = true; button.textContent = "Starting setup…"; issue.hidden = true;
    try {
      const { job } = await api("setup", { method: "POST", body: new URLSearchParams({ recipe: recipe.recipe, agree: "yes" }) });
      if (!validJob(job)) throw new TypeError("UGT did not return a run reference.");
      history.replaceState(null, "", "#job=" + encodeURIComponent(job));
      follow(recipe, job, true, "setup");
    } catch (error) { issue.textContent = error instanceof TypeError ? "Refresh this page to check whether setup started before trying again." : error.message; issue.hidden = false; button.disabled = false; button.textContent = "Continue with setup"; }
  });
  return el("details", { class: "setup-help disclosure", open: expanded }, el("summary", {}, "Need to finish setting up UGT?"),
    el("div", { class: "disclosure-body" }, el("p", { class: "note" }, currentPlan.windows_setup
      ? "UGT needs a Linux environment on this computer. Windows may ask for administrator permission and a restart. After restarting, return here to continue."
      : "UGT will check and repair the environment for this project. First-time setup may download files."), button, issue));
}
function ready(plan, recipe) {
  const form = el("form", { class: "workbench run-form" });
  form.addEventListener("submit", event => { event.preventDefault(); start(recipe, form); });
  const inputs = el("section", { class: "input-section" }, el("h2", {}, (recipe.inputs || []).length ? "What it needs" : "Get it running"),
    (recipe.inputs || []).length ? recipe.inputs.map(inputField) : el("p", { class: "section-note" }, recipe.kind === "service" ? "No files to choose. UGT will start the app on this computer." : "No files to choose. UGT will set up the tool on this computer."));
  const values = (recipe.settings || []).filter(setting => setting.kind === "value");
  const required = values.filter(setting => setting.required), optional = values.filter(setting => !setting.required);
  if (required.length) inputs.append(...required.map(valueField));
  const secrets = (recipe.settings || []).filter(setting => setting.kind === "secret");
  if (secrets.length) inputs.append(el("p", { class: "note secret-note" }, "UGT asks for " + secrets.map(setting => setting.what || setting.name).join(" and ") + " in its own window. Enter it there, never in chat."));
  if (optional.length) inputs.append(el("details", { class: "disclosure" }, el("summary", {}, "Change the defaults"), el("div", { class: "disclosure-body" }, optional.map(valueField))));
  if (recipe.kind === "service" && !recipe.installation_limit) inputs.append(el("details", { class: "disclosure" }, el("summary", {}, "Run a separate copy"),
    el("div", { class: "disclosure-body field" }, el("label", { for: "instance" }, "Copy name"), el("span", { class: "hint" }, "A new name has its own data and settings. Reuse the name to reopen that copy."),
      el("input", { type: "text", id: "instance", name: "instance", maxlength: 32, pattern: "[a-z0-9]+(-[a-z0-9]+)*", placeholder: "e.g. test", autocomplete: "off" }))));
  const outcomes = el("section", { class: "output-summary" }, el("h2", {}, "What you’ll get"), gives(recipe));
  const limits = [...(recipe.known_issues || [])]; if (recipe.installation_limit) limits.push(sentence(recipe.installation_limit));
  if (limits.length) inputs.append(el("section", { class: "run-notes" }, el("h2", {}, "Before you run it"), list(limits.map(issue => [issue, ""]))));
  inputs.append(el("details", { class: "access disclosure" }, el("summary", {}, "What this project can access"), el("div", { class: "disclosure-body" }, reaches(recipe))));
  outcomes.append(el("div", { class: "form-actions" }, el("button", { type: "submit", class: "primary" }, recipe.kind === "service" ? "Start the app" : "Run", icon("arrow")), el("p", { class: "note" }, "On this computer, in UGT’s private Linux engine.")));
  form.append(el("div", { class: "ready-grid" }, inputs, outcomes));
  previousForm = { recipe, form };
  showForm(recipe, form);
}
function showForm(recipe, form) {
  stopFollowing(); form.querySelector(".problem")?.remove();
  const button = form.querySelector("[type=submit]"); button.disabled = false;
  show(true, ...heading(recipe, sentence(recipe.what)), form, setupHelp(recipe), currentPlan.recipes.length > 1 ? el("button", { type: "button", class: "text-button choose-again", onclick: () => choose(currentPlan) }, "Choose another task") : null);
}
function another(recipe, instance, job) {
  history.replaceState(null, "", location.pathname + (job ? "#again=" + encodeURIComponent(job) : ""));
  if (previousForm?.recipe === recipe) showForm(recipe, previousForm.form);
  else {
    ready(currentPlan, recipe);
    const name = previousForm.form.elements.namedItem("instance");
    if (name && instance) { name.value = instance.split("@")[1] || ""; name.closest("details").open = !!name.value; }
  }
}
const validJob = id => typeof id === "string" && /^job_[0-7][0-9A-HJKMNP-TV-Z]{25}$/.test(id);
async function start(recipe, form) {
  form.querySelector("[type=submit]").disabled = true;
  const data = new FormData(form); data.append("recipe", recipe.recipe);
  for (const input of form.querySelectorAll("input[webkitdirectory]:not(:disabled)")) {
    data.delete(input.name);
    for (const file of input.files) {
      const relative = file.webkitRelativePath.split("/").slice(1).join("/");
      if (!relative) { form.querySelector("[type=submit]").disabled = false; form.append(problem("Choose the folder itself, then try again.")); return; }
      data.append(input.name, file, relative);
    }
  }
  stopFollowing();
  pending(recipe, "Starting your run", "Handing what you chose to UGT. Nothing else is needed from you yet.");
  try {
    const { job } = await api("run", { method: "POST", body: data });
    if (!validJob(job)) throw new TypeError("UGT did not return a run reference.");
    history.replaceState(null, "", "#job=" + encodeURIComponent(job));
    follow(recipe, job, true, "run");
  } catch (error) {
    if (error instanceof TypeError) {
      show(true, ...heading(recipe, "Let’s check whether it started"), panel(problem("The connection dropped before UGT could confirm. Refresh to look for the same run before trying again."), el("div", { class: "actions" }, retryPage(), returnLink(recipe))));
    } else { showForm(recipe, form); form.querySelector(".form-actions").before(problem(sentence(error.message))); }
  }
}

function pending(recipe, title, detail, id = "") {
  const operation = el("section", { class: "workbench run-panel", "data-job": id, "data-recipe": recipe?.recipe || "" },
    el("div", { class: "run-heading" }, el("span", { class: "activity-mark", "aria-hidden": "true" }), el("div", {}, el("p", { class: "phase-label" }, "In progress"), el("h2", { class: "phase-title", role: "status" }, title))),
    el("p", { class: "run-description" }, detail), el("p", { class: "elapsed", "aria-live": "off" }),
    el("ol", { class: "milestones", "aria-label": "Run progress" }),
    el("details", { class: "disclosure activity-details", hidden: true }, el("summary", {}, "Show run details"), el("div", { class: "activity-log" })));
  show(true, ...heading(recipe, recipe ? sentence(recipe.what) : "Checking your run"), operation);
  return operation;
}
function phase(job, recipe) {
  const last = (job.events || []).at(-1);
  if (job.kind === "setup") return { index: 0, title: "Setting up UGT", detail: "UGT is preparing the environment on this computer." };
  if (!last || !["pull", "build", "task", "sample", "check", "sample-check"].includes(last.step)) return { index: 0, title: "Getting the project ready", detail: "UGT is preparing the environment and what you chose." };
  if (last.step === "pull" || last.step === "build") return { index: 0, title: last.step === "pull" ? "Downloading the project" : "Building the project", detail: "UGT is getting the files this project needs. Your run will continue here." };
  if (last.step === "check" || last.step === "sample-check") return { index: last.message === "exit 0" ? 3 : 2, title: last.message === "exit 0" ? "Getting your result ready" : "Checking the result", detail: last.message === "exit 0" ? "The check passed. UGT is making the result available on this computer." : "UGT is checking that the project actually worked." };
  return { index: 1, title: job.kind === "run" && recipe.kind === "service" ? "Starting " + nameOf(recipe) : "Running " + nameOf(recipe), detail: "The project is working on this computer. You can leave this page open." };
}
function updateClock() {
  for (const clock of page.querySelectorAll("[data-started]")) clock.textContent = seconds((Date.now() - Date.parse(clock.dataset.started)) / 1000) + " elapsed";
}
function eventList(events) {
  return el("ol", { class: "steps" }, events.map(event => el("li", {}, event.step ? el("span", { class: "step" }, event.step) : null, el("span", {}, sentence(event.message)))));
}
function running(recipe, job) {
  let view = page.querySelector(".run-panel");
  if (!view || view.dataset.job !== job.id) view = pending(recipe, "Getting the project ready", "UGT is preparing this run.", job.id);
  if (view.dataset.recipe !== recipe.recipe) {
    const oldHeading = page.querySelector(".view-heading"), focus = oldHeading.contains(document.activeElement);
    const newHeading = heading(recipe, sentence(recipe.what))[0]; oldHeading.replaceWith(newHeading);
    view.dataset.recipe = recipe.recipe; if (focus) newHeading.querySelector("h1").focus({ preventScroll: true });
  }
  const state = phase(job, recipe);
  const title = view.querySelector(".phase-title"); if (title.textContent !== state.title) title.textContent = state.title;
  view.querySelector(".run-description").textContent = state.detail;
  const clock = view.querySelector(".elapsed"); clock.dataset.started = job.started; updateClock();
  const track = view.querySelector(".milestones"); track.hidden = job.kind === "setup";
  if (track.dataset.phase !== String(state.index)) {
    track.dataset.phase = String(state.index);
    track.replaceChildren(...["Get ready", recipe.kind === "service" ? "Start" : "Run", "Check"].map((label, index) => el("li", { class: index < state.index ? "complete" : index === state.index ? "current" : "", "aria-current": index === state.index ? "step" : null }, el("span", { class: "milestone-mark", "aria-hidden": "true" }, index < state.index ? icon("check") : null), label)));
  }
  const details = view.querySelector(".activity-details"), log = view.querySelector(".activity-log"), signature = JSON.stringify(job.events || []);
  details.hidden = !(job.events || []).length;
  if (log.dataset.events !== signature) { log.dataset.events = signature; log.replaceChildren(eventList(job.events || [])); }
}
async function follow(recipe, id, first, kind, version) {
  if (first) { stopFollowing(); version = followVersion; pending(recipe, kind === "setup" ? "Starting setup" : kind === "run" ? "Getting the project ready" : "Checking your run", "UGT has your request. Checking its current state.", id); }
  let job;
  try { job = await api("job?id=" + encodeURIComponent(id)); }
  catch {
    if (version !== followVersion) return;
    show(false, ...heading(recipe, "We lost touch with UGT"), panel(problem("Refresh to check the same run. If UGT has closed, reopen the project from the website."), el("div", { class: "actions" }, retryPage(), recipe ? returnLink(recipe, id) : null))); return;
  }
  if (version !== followVersion) return;
  recipe = currentPlan.recipes.find(candidate => candidate.recipe === job.recipe);
  if (!recipe) { stopFollowing(); show(true, ...heading(null, "This run belongs to another project")); return; }
  if (job.state === "queued" || job.state === "running") { running(recipe, job); pollTimer = setTimeout(() => follow(recipe, id, false, job.kind, version), 1000); return; }
  stopFollowing();
  if (job.kind === "setup") {
    const result = job.result || {};
    if (result.ready && job.state === "succeeded") show(true, ...heading(recipe, "UGT is ready", "Continue with the project you chose.", true), panel(el("button", { type: "button", class: "primary", onclick: () => another(recipe) }, "Continue with my project", icon("arrow"))));
    else if (result.needs_restart) show(true, ...heading(recipe, "Restart Windows to continue"), panel(el("p", {}, "After signing back in, reopen this project and choose Continue with setup. Your project stays saved on the website."), el("div", { class: "actions" }, returnLink(recipe))));
    else show(true, ...heading(recipe, "Setup needs attention"), panel(problem(job.error || (result.next || []).join(" ") || "UGT could not finish setup on this computer."), setupHelp(recipe, true), el("div", { class: "actions" }, returnLink(recipe))));
    return;
  }
  finished(recipe, job);
}

function commandBlock(command) {
  const button = el("button", { type: "button", class: "text-button copy-command" }, "Copy");
  const status = el("span", { class: "copy-status", role: "status" });
  button.addEventListener("click", async () => { try { await navigator.clipboard.writeText(command.type); status.textContent = "Copied"; } catch { status.textContent = "Select the command to copy it."; } });
  return el("div", { class: "command" }, el("div", { class: "command-heading" }, el("p", {}, sentence(command.what)), button, status), el("pre", {}, el("code", {}, command.type)), command.note ? el("p", { class: "note" }, command.note) : null);
}
function finished(recipe, job) {
  const result = job.result || {};
  if (job.state !== "succeeded" || !result.passed) {
    show(true, ...heading(recipe, "This run needs attention"), panel(problem(sentence(job.error) || "The check did not pass on what the project made."),
      el("div", { class: "actions" }, el("button", { type: "button", class: "primary", onclick: () => another(recipe, result.installation, job.id) }, recipe.kind === "service" ? "Review setup and try again" : "Change inputs and try again")),
      el("details", { class: "disclosure" }, el("summary", {}, "Show run details"), eventList(job.events || [])), el("div", { class: "result-footer" }, returnLink(recipe, job.id))), setupHelp(recipe)); return;
  }
  const content = [];
  if (result.installation) content.push(el("p", { class: "installation note" }, "Copy: " + (result.installation.split("@")[1] || result.installation)));
  if (result.results) {
    const reveal = el("button", { type: "button", class: "primary" }, fileManager, icon("arrow"));
    const issue = problem(""); issue.hidden = true;
    reveal.addEventListener("click", async () => {
      reveal.disabled = true;
      try { await api("reveal?id=" + encodeURIComponent(job.id), { method: "POST" }); issue.hidden = true; }
      catch { issue.textContent = "Open this folder yourself: " + result.results; issue.hidden = false; }
      finally { reveal.disabled = false; }
    });
    content.push(el("section", { class: "result-group" }, el("h2", {}, "Your files"), list((result.files || []).map(file => [file, ""]), "files"),
      el("div", { class: "result-actions" }, reveal), el("p", { class: "saved-location note" }, "Saved to ", el("span", {}, inUGT(result.results))), issue));
  }
  if ((result.pages || []).length) content.push(el("section", { class: "result-group" }, el("h2", {}, "Open your app"), el("div", { class: "app-links" }, result.pages.map(item => el("a", { class: "primary", href: item.url, target: "_blank", rel: "noreferrer" }, sentence(item.what), icon("arrow"))))));
  if ((result.endpoints || []).length) content.push(el("section", { class: "result-group" }, el("h2", {}, "For other software"), list(result.endpoints.map(item => [item.url, sentence(item.what)]))));
  if ((result.commands || []).length) content.push(el("section", { class: "result-group" }, el("h2", {}, "Use from your terminal"), result.commands.map(commandBlock)));
  if ((result.mcp_servers || []).length) content.push(el("section", { class: "result-group" }, el("h2", {}, "Ask your AI app"), list(result.mcp_servers.map(item => [sentence(item.what), item.note]))));
  if (recipe.kind === "service") {
    const stop = el("button", { type: "button", class: "secondary" }, "Stop this copy");
    const status = el("p", { class: "note", role: "status" }, "Stopping keeps its data on this computer.");
    stop.addEventListener("click", async () => {
      stop.disabled = true; stop.textContent = "Stopping…";
      try { await api("stop?id=" + encodeURIComponent(job.id), { method: "POST" }); history.replaceState(null, "", location.pathname + "#again=" + encodeURIComponent(job.id)); stopped(recipe, result.installation, job.id); }
      catch (error) { status.textContent = "UGT couldn’t stop the app: " + error.message; stop.disabled = false; stop.textContent = "Stop this copy"; }
    });
    content.push(el("div", { class: "service-controls" }, stop, status));
  }
  content.push(el("div", { class: "result-footer" }, recipe.kind !== "service" ? el("button", { type: "button", class: "secondary", onclick: () => another(recipe, undefined, job.id) }, "Run again") : null, returnLink(recipe)));
  show(true, ...heading(recipe, result.files?.length ? result.files.length === 1 ? "Your file is ready" : "Your files are ready" : result.kind === "service" ? "Your app is ready" : "Ready to use", "Completed on this computer in " + seconds(result.seconds || 0) + ".", true), panel(content));
}
function stopped(recipe, instance, job) {
  show(true, ...heading(recipe, "You stopped this copy", "Its data stays here. Start the same copy to use it again."), panel(el("button", { type: "button", class: "primary", onclick: () => another(recipe, instance, job) }, "Back to the app", icon("arrow")), el("div", { class: "result-footer" }, returnLink(recipe))));
}

async function main() {
  setInterval(() => fetch("api/alive").catch(() => {}), 15000);
  setInterval(updateClock, 1000);
  let plan;
  try { plan = await api("plan"); }
  catch { show(true, ...heading(null, "We lost touch with UGT"), panel(problem("Refresh this page. If UGT has closed, reopen your project from the website."), el("div", { class: "actions" }, retryPage()))); return; }
  currentPlan = plan; folder = plan.folder;
  if (plan.error) { show(true, ...heading(null, "UGT cannot open this link"), panel(problem(sentence(plan.error)), el("div", { class: "actions" }, retryPage()))); return; }
  const route = new URLSearchParams(location.hash.slice(1)), job = route.get("job") || route.get("again") || plan.last_job;
  if (job) {
    if (route.get("again")) {
      try { const record = await api("job?id=" + encodeURIComponent(job)), recipe = plan.recipes.find(item => item.recipe === record.recipe); if (recipe) { another(recipe, record.result?.installation, record.id); return; } } catch { /* Follow the same job and offer recovery. */ }
    }
    follow(null, job, true); return;
  }
  if (plan.recipes.length === 1) ready(plan, plan.recipes[0]); else choose(plan);
}
main();
