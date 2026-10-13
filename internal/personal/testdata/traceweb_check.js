// Renders traceweb.html's page script against a minimal DOM and checks the
// text it produces (TestTraceWebRenders). The DOM keeps the real append
// semantics: a non-Node argument is printed as String(x), so passing an array
// shows "[object HTMLDivElement]" like a browser does.
//   node traceweb_check.js <traceweb.html> <session.json> <env.json>
const fs = require("fs");
const vm = require("vm");
const [htmlPath, sessionPath, envPath] = process.argv.slice(2);

class Node {
  constructor(tag) { this.tag = tag; this.children = []; this.attrs = {}; this.style = {}; this.className = ""; this.dataset = {}; this.classList = { toggle() {}, add() {} }; }
  append(...xs) { for (const x of xs) this.children.push(x instanceof Node ? x : new Text(String(x))); }
  replaceChildren(...xs) { this.children = []; this.append(...xs); }
  setAttribute(k, v) { this.attrs[k] = v; }
  addEventListener() {}
  set textContent(t) { this.children = [new Text(t)]; }
  get textContent() { return this.children.map(c => c.textContent).join(""); }
}
class Text extends Node { constructor(t) { super("#text"); this.t = t; } get textContent() { return this.t; } }
const blocks = new Set(["div", "p", "h3", "h4", "tr", "pre", "summary"]);
const ids = {};
Object.assign(globalThis, {
  Node,
  document: { createElement: t => new Node(t), createTextNode: t => new Text(t), getElementById: id => (ids[id] ||= new Node("div")), querySelectorAll: () => [] },
  location: { hash: "" }, history: { replaceState() {} }, fetch: () => new Promise(() => {}),
  __dump: n => n instanceof Text ? n.textContent : n.children.map(__dump).join(blocks.has(n.tag) ? "\n" : ""),
  __session: JSON.parse(fs.readFileSync(sessionPath, "utf8")),
  __env: JSON.parse(fs.readFileSync(envPath, "utf8")),
});

const page = /<script>([\s\S]*)<\/script>/.exec(fs.readFileSync(htmlPath, "utf8"))[1];
const checks = `
data = __session; env = __env;
data.events = data.events || []; data.entries = data.entries || [];
for (const en of data.entries) entryBySeq.set(en.seq, en);
t0 = Math.min(...data.events.map(at));
let bad = 0;
const check = (name, ok) => { console.log((ok ? "PASS " : "FAIL ") + name); if (!ok) bad++; };
tab = "env";
const main = new Node("main");
renderEnv(main);
let text = __dump(main);
check("env tab: no [object ...]", !text.includes("[object"));
check("env tab: tools listed", env.tools.every(t => text.includes(t.name)));
const ctx = data.events.find(e => e.kind === "context");
const box = new Node("div");
showDetail(box, ctx);
text = __dump(box);
check("context: no [object ...]", !text.includes("[object"));
check("context: tool definitions named", text.includes("工具定义（") && ctx.attrs.tool_names.every(n => text.includes(n)));
delete ctx.attrs.tool_names; // recorded by an older suag
showDetail(box, ctx);
text = __dump(box);
check("old context: the tools the session called", text.includes("没有工具名单") && text.includes("本会话调用过的工具："));
for (const e of data.events) { const b = new Node("div"); showDetail(b, e); if (__dump(b).includes("[object")) check("detail " + e.kind + ": no [object ...]", false); }
process.exitCode = bad ? 1 : 0;
`;
vm.runInThisContext(page + checks);
