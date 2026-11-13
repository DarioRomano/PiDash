// Picker for the folders and albums of the background slideshow. Opened from the
// phone remote and from the settings page.
import type { PhotoFolder, PhotoSelection, PhotosStatus } from "./types";

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;
const statusEl = $("status");
const tree = $("tree");
const recursiveBox = $<HTMLInputElement>("recursive");
const chosenBox = $("chosen-box");
const chosenList = $("chosen");
const saveBtn = $<HTMLButtonElement>("save");
const summary = $("summary");

/** The folders ticked on this page, by id. */
const chosen = new Map<string, PhotoFolder>();
/** What the TV is using right now, to know whether there is anything to save. */
let saved: PhotoSelection = { folders: [], recursive: true };

let saving = false;

interface Node {
  folder: PhotoFolder;
  /** Ids of all folders above this one, nearest last. */
  ancestors: string[];
  box: HTMLInputElement;
  row: HTMLElement;
}
const nodes: Node[] = [];

function selectionKey(sel: PhotoSelection) {
  return JSON.stringify([sel.recursive, sel.folders.map((f) => f.id).sort()]);
}

function current(): PhotoSelection {
  return { recursive: recursiveBox.checked, folders: [...chosen.values()] };
}

// --- Rendering ----------------------------------------------------------------

function refresh() {
  const recursive = recursiveBox.checked;
  for (const n of nodes) {
    // With "include the folders inside them", ticking a folder covers everything below it.
    const covered = recursive && n.ancestors.some((id) => chosen.has(id));
    n.box.checked = covered || chosen.has(n.folder.id);
    n.box.disabled = covered;
    n.row.classList.toggle("covered", covered);
  }

  chosenBox.hidden = chosen.size === 0;
  chosenList.replaceChildren(
    ...[...chosen.values()]
      .sort((a, b) => a.path.localeCompare(b.path))
      .map((f) => {
        const li = document.createElement("li");
        const span = document.createElement("span");
        span.textContent = f.path;
        const remove = document.createElement("button");
        remove.textContent = "×";
        remove.setAttribute("aria-label", `Remove ${f.path}`);
        remove.addEventListener("click", () => {
          chosen.delete(f.id);
          refresh();
        });
        li.append(span, remove);
        return li;
      }),
  );

  const dirty = selectionKey(current()) !== selectionKey(saved);
  saveBtn.disabled = !dirty || saving;
  saveBtn.textContent = saving ? "Saving…" : "Use these photos";
  summary.textContent = chosen.size === 0 ? "Nothing chosen" : `${chosen.size} chosen`;
}

function showStatus(st: PhotosStatus) {

  $("main").hidden = !st.enabled;
  $("footer").hidden = !st.enabled;
  $("disabled").hidden = st.enabled;
  statusEl.classList.remove("error", "ok");
  if (!st.enabled) {
    statusEl.textContent = "Not set up";
    return;
  }
  if (st.error) {
    statusEl.textContent = st.error;
    statusEl.classList.add("error");
  } else if (st.indexing) {
    statusEl.textContent = `${st.source} · reading your choice…`;
  } else {
    const n = st.count.toLocaleString();
    statusEl.textContent = `${st.source} · ${n} ${st.count === 1 ? "photo" : "photos"} in the slideshow`;
    if (st.count > 0) statusEl.classList.add("ok");
  }
}

// --- Folder tree, loaded one level at a time -------------------------------------

async function loadChildren(parent: string, container: HTMLElement, ancestors: string[], depth: number) {
  const note = document.createElement("div");
  note.className = "note";
  note.style.paddingLeft = `${48 + depth * 22}px`;
  note.textContent = "Loading…";
  container.replaceChildren(note);
  try {
    const res = await fetch(`/api/photos/folders?parent=${encodeURIComponent(parent)}`);
    if (!res.ok) throw new Error((await res.text()).trim());
    const data: { folders: PhotoFolder[] } = await res.json();
    if (data.folders.length === 0) {
      note.textContent = depth === 0 ? "No folders found." : parent === "a:" ? "No albums yet." : "No folders inside.";
      return;
    }
    container.replaceChildren(...data.folders.map((f) => makeNode(f, ancestors, depth)));
    refresh();
  } catch (err) {
    note.classList.add("error");
    note.textContent = err instanceof Error && err.message ? err.message : "Could not load the folders.";
  }
}

function makeNode(folder: PhotoFolder, ancestors: string[], depth: number): HTMLElement {
  const node = document.createElement("div");
  node.className = "node";

  const row = document.createElement("div");
  row.className = "row";
  row.style.paddingLeft = `${depth * 22}px`;

  const expand = document.createElement("button");
  expand.className = "expand";
  expand.textContent = "›";
  expand.setAttribute("aria-label", `Show what is inside ${folder.name}`);
  if (folder.leaf) {
    // An album has nothing to open; the empty space keeps the names aligned.
    expand.disabled = true;
    expand.classList.add("leaf");
    expand.setAttribute("aria-hidden", "true");
  }

  const label = document.createElement("label");
  const box = document.createElement("input");
  box.type = "checkbox";
  const name = document.createElement("span");
  name.className = "name";
  name.textContent = folder.name;
  label.append(box, name);
  if (folder.detail) {
    const detail = document.createElement("span");
    detail.className = "detail";
    detail.textContent = folder.detail;
    label.append(detail);
  }
  row.append(expand, label);

  const children = document.createElement("div");
  children.className = "children";
  node.append(row, children);

  let loaded = false;
  expand.addEventListener("click", () => {
    const open = children.classList.toggle("open");
    expand.classList.toggle("open", open);
    if (open && !loaded) {
      loaded = true;
      void loadChildren(folder.id, children, [...ancestors, folder.id], depth + 1);
    }
  });
  box.addEventListener("change", () => {
    if (box.checked) chosen.set(folder.id, folder);
    else chosen.delete(folder.id);
    refresh();
  });

  nodes.push({ folder, ancestors, box, row });
  return node;
}

// --- Saving ---------------------------------------------------------------------

recursiveBox.addEventListener("change", refresh);

saveBtn.addEventListener("click", async () => {
  saving = true;
  refresh();
  try {
    const res = await fetch("/api/photos/selection", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(current()),
    });
    if (!res.ok) throw new Error((await res.text()).trim());
    // Only the saved selection is taken from this reply. Its photo count is
    // already out of date, because the Pi starts re-reading the folders at once
    // and reports progress over the live connection, which can arrive first.
    saved = ((await res.json()) as PhotosStatus).selection;
    window.setTimeout(refreshStatus, 1500); // in case the live connection is down
  } catch (err) {
    statusEl.classList.add("error");
    statusEl.textContent = err instanceof Error && err.message ? err.message : "Could not save.";
  } finally {
    saving = false;
    refresh();
  }
});

async function refreshStatus() {
  try {
    showStatus(await (await fetch("/api/photos")).json());
  } catch {
    /* the live connection reports it when the backend is back */
  }
}

// --- Start ------------------------------------------------------------------------

async function start() {
  let st: PhotosStatus;
  try {
    st = await (await fetch("/api/photos")).json();
  } catch {
    statusEl.classList.add("error");
    statusEl.textContent = "The PiDash backend is not reachable.";
    return;
  }
  saved = st.selection;
  for (const f of st.selection.folders) chosen.set(f.id, f);
  recursiveBox.checked = st.selection.recursive;
  showStatus(st);
  if (!st.enabled) return;
  refresh();
  void loadChildren("", tree, [], 0);

  // Live progress while the Pi reads the chosen folders.
  const events = new EventSource("/events");
  events.addEventListener("photos", (e) => showStatus(JSON.parse((e as MessageEvent).data)));
}

void start();
