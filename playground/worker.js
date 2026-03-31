// Web Worker that loads Go WASM and runs pgfmt off the main thread.
importScripts("wasm_exec.js");

onPgfmtReady = function () {
  postMessage({ type: "ready" });
};

onPgfmtWarn = function (msg) {
  postMessage({ type: "warn", message: msg });
};

(async function () {
  const go = new Go();
  const result = await WebAssembly.instantiateStreaming(
    fetch("pgfmt.wasm"),
    go.importObject
  );
  go.run(result.instance);
})();

onmessage = function (e) {
  const { id, sql } = e.data;
  try {
    const res = pgfmtFormat(sql);
    postMessage({ type: "result", id, result: res.result, error: res.error });
  } catch (err) {
    postMessage({ type: "result", id, error: err.toString() });
  }
};
