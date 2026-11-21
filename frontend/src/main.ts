// The page only shows state and forwards clicks to the Go service. All text
// from Go is set with textContent, never innerHTML (CR-0.1.0-02 D16).
import { Events } from "@wailsio/runtime";
import * as Svc from "../bindings/github.com/naseebnaushad/tandem-for-claude/ui/service";
import type { ProfileView, Settings } from "../bindings/github.com/naseebnaushad/tandem-for-claude/internal/app/models";

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

/** Builds an element with optional class and text. */
function el<K extends keyof HTMLElementTagNameMap>(tag: K, cls = "", text = ""): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text) e.textContent = text;
  return e;
}

function button(label: string, onClick: () => void, cls = ""): HTMLButtonElement {
  const b = el("button", cls, label);
  b.type = "button";
  b.addEventListener("click", onClick);
  return b;
}

let noticeTimer: number | undefined;
function notice(msg: string) {
  const n = $("notice");
  n.textContent = msg;
  n.hidden = false;
  window.clearTimeout(noticeTimer);
  noticeTimer = window.setTimeout(() => (n.hidden = true), 8000);
}

function errText(e: unknown): string {
  if (e instanceof Error) return e.message;
  if (typeof e === "object" && e && "message" in e) return String((e as { message: unknown }).message);
  return String(e);
}

/** Runs a call that returns nothing; true when it worked, false after showing the error. */
async function ok(fn: () => Promise<unknown>): Promise<boolean> {
  try {
    await fn();
    return true;
  } catch (e) {
    notice(errText(e));
    return false;
  }
}

async function run<T>(fn: () => Promise<T>): Promise<T | undefined> {
  try {
    return await fn();
  } catch (e) {
    notice(errText(e));
    return undefined;
  }
}

// ---------- dialog ----------

/** Shows the dialog; resolves true when OK is pressed. */
function dialog(title: string, body: Node[], ok = "OK", danger = false): Promise<boolean> {
  const d = $<HTMLDialogElement>("dlg");
  $("dlg-title").textContent = title;
  $("dlg-body").replaceChildren(...body);
  const okBtn = $<HTMLButtonElement>("dlg-ok");
  okBtn.textContent = ok;
  okBtn.className = danger ? "danger" : "primary";
  return new Promise((resolve) => {
    d.addEventListener("close", () => resolve(d.returnValue === "ok"), { once: true });
    d.showModal();
    const first = d.querySelector<HTMLInputElement>("input[type=text]");
    first?.focus();
  });
}

function para(text: string, cls = "muted") {
  return el("p", cls, text);
}

// ---------- tabs ----------

const loaders: Record<string, () => void> = {};
document.querySelectorAll<HTMLButtonElement>(".tabs button").forEach((b) =>
  b.addEventListener("click", () => {
    document.querySelectorAll<HTMLButtonElement>(".tabs button").forEach((x) => x.setAttribute("aria-selected", String(x === b)));
    document.querySelectorAll<HTMLElement>(".tab").forEach((t) => (t.hidden = t.id !== "tab-" + b.dataset.tab));
    loaders[b.dataset.tab ?? ""]?.();
  }),
);

// ---------- profiles ----------

async function loadProfiles() {
  const list = await run(() => Svc.Profiles());
  if (!list) return;
  $("profiles").replaceChildren(...list.map(card));
}
loaders.profiles = loadProfiles;

function card(p: ProfileView): HTMLElement {
  const c = el("div", "card");
  c.append(el("h3", "", p.name));
  const meta = el("div", "meta");
  if (p.kind === "desktop") {
    const dot = el("span", "dot" + (p.running ? " on" : ""));
    meta.append(dot, el("span", "", p.running ? "Desktop · open" : "Desktop · closed"));
  } else {
    meta.append(el("span", "", "Terminal (Claude Code)"));
  }
  c.append(meta);
  if (p.kind === "cli" && p.command) {
    c.append(el("div", "cmd", p.command));
  }
  const actions = el("div", "actions");
  actions.append(button(p.kind === "cli" ? "Open terminal" : "Open", async () => {
    await ok(() => Svc.OpenProfile(p.id));
    window.setTimeout(loadProfiles, 2500);
  }, "primary"));
  if (p.kind === "desktop" && !p.builtin) actions.append(button("Sign in", () => signIn(p)));
  actions.append(button("Rename", () => rename(p)));
  actions.append(button("Folder", () => ok(() => Svc.OpenFolder(p.id))));
  if (!p.builtin) actions.append(button("Delete", () => remove(p), "danger"));
  c.append(actions);
  return c;
}

$("new-profile").addEventListener("click", async () => {
  const name = el("input");
  name.type = "text";
  name.maxLength = 40;
  name.placeholder = "e.g. Work, Seat 2";
  const choice = el("div", "choice");
  for (const [value, label, help] of [
    ["desktop", "Desktop", "A separate Claude desktop window"],
    ["cli", "Terminal", "Claude Code in a terminal"],
  ]) {
    const l = el("label");
    const r = el("input");
    r.type = "radio";
    r.name = "kind";
    r.value = value;
    r.checked = value === "desktop";
    l.append(r, " ", label, el("small", "", help));
    choice.append(l);
  }
  if (!(await dialog("New profile", [para("Name"), name, choice], "Create"))) return;
  const kind = choice.querySelector<HTMLInputElement>("input:checked")?.value ?? "desktop";
  const created = await run(() => Svc.CreateProfile(name.value, kind));
  if (!created) return;
  await loadProfiles();
  if (kind === "desktop") {
    signIn({ ...created, folder: "", running: false, command: "" } as ProfileView);
  } else {
    notice(`Created "${created.name}". Click Open terminal and log in with the seat for this profile.`);
  }
});

async function signIn(p: ProfileView) {
  const others = (await run(() => Svc.SignInOthers(p.id))) ?? [];
  const body = [
    para(`Sign in to "${p.name}" with the seat it should use. You only need to do this once.`),
    para("Claude sends the sign-in to the first Claude window that is open, so the others close for a moment."),
  ];
  if (others.length) body.push(para("These windows will close and reopen afterwards: " + others.join(", "), "warn"));
  if (!(await dialog("Sign in", body, others.length ? "Close them and continue" : "Continue"))) return;
  if (!(await ok(() => Svc.SignInStart(p.id)))) return;
  await dialog("Sign in", [para(`Sign in inside the "${p.name}" window. Click Done when you see your chats.`)], "Done");
  await ok(() => Svc.SignInDone());
  window.setTimeout(loadProfiles, 2500);
}

async function rename(p: ProfileView) {
  const name = el("input");
  name.type = "text";
  name.maxLength = 40;
  name.value = p.name;
  if (!(await dialog("Rename profile", [name], "Rename"))) return;
  await ok(() => Svc.RenameProfile(p.id, name.value));
  loadProfiles();
}

async function remove(p: ProfileView) {
  const del = el("input");
  del.type = "checkbox";
  const l = el("label", "check");
  l.append(del, "Also delete its folder and sign-in");
  const confirmed = await dialog(`Delete "${p.name}"?`, [
    para("Your chats stay; they are shared by every profile."),
    l,
  ], "Delete", true);
  if (!confirmed) return;
  await ok(() => Svc.DeleteProfile(p.id, del.checked));
  loadProfiles();
}

// ---------- sync ----------

function ago(iso: string): string {
  if (!iso) return "never";
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  if (s < 60) return "just now";
  if (s < 3600) return `${Math.round(s / 60)} min ago`;
  return new Date(iso).toLocaleString();
}

function showSync(v: Awaited<ReturnType<typeof Svc.SyncStatus>>) {
  $("sync-headline").textContent = v.paused
    ? "Sync is paused"
    : `${v.chats} chats on every seat`;
  const parts = [`Last synced ${ago(v.lastSync)}`];
  if (!v.enabled) parts.push("automatic sync is off");
  else parts.push(`every ${v.intervalMinutes} min`);
  if (v.pending) parts.push(`${v.pending} waiting`);
  $("sync-sub").textContent = parts.join(" · ");
  const items: HTMLElement[] = [];
  if (v.paused) items.push(el("li", "", "Some chat list files have a format this version doesn't know (Claude may have been updated). Nothing is written until Tandem for Claude is updated."));
  for (const w of [...(v.warnings ?? []), ...(v.problems ?? [])]) items.push(el("li", "", w));
  if (!items.length) items.push(el("li", "empty", "No warnings."));
  $("sync-warnings").replaceChildren(...items);
}

async function loadSync() {
  const v = await run(() => Svc.SyncStatus());
  if (v) showSync(v);
}
loaders.sync = loadSync;

$("sync-now").addEventListener("click", async () => {
  const b = $<HTMLButtonElement>("sync-now");
  b.disabled = true;
  const v = await run(() => Svc.SyncNow());
  b.disabled = false;
  if (v) {
    showSync(v);
    notice(v.applied ? `Synced ${v.applied} change(s). Restart a Claude window to see new chats.` : "Everything was already in sync.");
  }
});

// ---------- backups ----------

async function loadBackups() {
  const list = (await run(() => Svc.Backups())) ?? [];
  const items = list.map((id) => {
    const li = el("li");
    li.append(el("span", "", id));
    li.append(button("Restore", async () => {
      if (!(await dialog("Restore backup?", [para(`Put back the chat lists from ${id}. The current ones are kept next to them, not deleted. Quit every Claude window first.`)], "Restore"))) return;
      const r = await run(() => Svc.RestoreBackup(id));
      if (r) notice(`Restored: ${r.join(", ")}`);
      loadBackups();
    }));
    return li;
  });
  if (!items.length) items.push(el("li", "empty", "No backups yet."));
  $("backups").replaceChildren(...items);
}
loaders.backups = loadBackups;

// ---------- settings ----------

async function loadSettings() {
  const s = await run(() => Svc.Settings());
  if (!s) return;
  $<HTMLInputElement>("set-sync").checked = s.syncEnabled;
  $<HTMLInputElement>("set-interval").value = String(s.intervalMinutes);
  $<HTMLInputElement>("set-login").checked = s.startAtLogin;
  loadRetention();
}
loaders.settings = loadSettings;

async function loadRetention() {
  const d = await run(() => Svc.Retention());
  if (d !== undefined) $<HTMLInputElement>("set-retention").value = String(d);
}

$("save-retention").addEventListener("click", async () => {
  const days = Math.round(Number($<HTMLInputElement>("set-retention").value));
  if (await ok(() => Svc.SetRetention(days))) notice(`Claude Code will keep chats for ${days} days.`);
  loadRetention();
});

$("save-settings").addEventListener("click", async () => {
  const s: Settings = {
    syncEnabled: $<HTMLInputElement>("set-sync").checked,
    intervalMinutes: Math.min(60, Math.max(1, Number($<HTMLInputElement>("set-interval").value) || 5)),
    startAtLogin: $<HTMLInputElement>("set-login").checked,
  };
  if (await ok(() => Svc.SaveSettings(s))) notice("Settings saved.");
  loadSettings();
});

// ---------- start ----------

Events.On("synced", () => {
  if (!$("tab-sync").hidden) loadSync();
});
Events.On("notice", (e: { data: unknown }) => notice(String(e.data)));

(async () => {
  $("no-claude").hidden = (await run(() => Svc.ClaudeFound())) !== false;
  loadProfiles();
})();
