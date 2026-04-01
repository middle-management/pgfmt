// Web Worker: pg-query-emscripten parses SQL, Go WASM prints it.
import pgQueryInit from "https://esm.sh/pg-query-emscripten@5.1.0";

var pgQuery;
var printerReady = false;

globalThis.onPgfmtReady = function () {
  printerReady = true;
  if (pgQuery) postMessage({ type: "ready" });
};

// Expose PL/pgSQL parsing to Go printer via JS callback.
globalThis.pgfmtParsePlPgSQL = function (sql) {
  try {
    var result = pgQuery.parsePlpgsql(sql);
    if (result.error) {
      return { error: result.error.message || String(result.error) };
    }
    return { result: JSON.stringify(result.plpgsql_funcs) };
  } catch (err) {
    return { error: err.toString() };
  }
};

// Load pg-query-emscripten.
pgQuery = await new pgQueryInit();
if (printerReady) postMessage({ type: "ready" });

// Load Go WASM (printer only).
// importScripts doesn't work in module workers, so we fetch and eval.
const wasm_exec = await fetch("wasm_exec.js").then((r) => r.text());
eval(wasm_exec);

var go = new Go();
var result = await WebAssembly.instantiateStreaming(
  fetch("pgfmt.wasm"),
  go.importObject
);
go.run(result.instance);

onmessage = function (e) {
  var id = e.data.id;
  try {
    var parsed = pgQuery.parse(e.data.sql);
    if (parsed.error) {
      postMessage({ type: "result", id: id, error: parsed.error.message });
      return;
    }
    var printed = pgfmtPrintParseResult(JSON.stringify(parsed.parse_tree));
    if (printed === undefined) {
      postMessage({ type: "result", id: id, error: "printer crashed" });
      return;
    }
    postMessage({
      type: "result",
      id: id,
      result: printed.result,
      error: printed.error,
    });
  } catch (err) {
    postMessage({ type: "result", id: id, error: err.toString() });
  }
};
