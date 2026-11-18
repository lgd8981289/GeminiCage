import assert from "node:assert/strict";
import test from "node:test";


test("paired phone shortcut opens Gemini inside the guest browser", async (t) => {
  globalThis.location = new URL("http://localhost/");
  t.after(() => { delete globalThis.location; });
  const {connectDisplay} = await import("./mobile-cli/browser-session.mjs");
  t.mock.method(globalThis, "fetch", async (url, options) => {
    assert.equal(url, "/api/mobile/browser/open");
    observed = JSON.parse(options.body);
    return new Response(null, {status: 423});
  });
  const previous = globalThis.ResizeObserver;
  globalThis.ResizeObserver = class { observe() {} disconnect() {} };
  t.after(() => { if (previous) globalThis.ResizeObserver = previous; else delete globalThis.ResizeObserver; });
  let observed;
  const handle = connectDisplay({clientWidth:390,clientHeight:660}, () => {}, "phone-test", true, "claude");
  try {
    await new Promise(resolve => setImmediate(resolve));
    assert.equal(observed?.url, "https://gemini.google.com/");
  } finally { handle.close(); }
});
