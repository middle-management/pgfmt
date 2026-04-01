// Web Worker: pg-query-emscripten parses SQL, Go WASM prints it.
import pgQueryInit from "https://esm.sh/pg-query-emscripten@5.1.0";

// Load Go WASM runtime (sets globalThis.Go).
import "./wasm_exec.js";

let pgQuery;
let printerReady = false;

globalThis.onPgfmtReady = () => {
  printerReady = true;
  if (pgQuery) postMessage({ type: "ready" });
};

// Expose PL/pgSQL parsing to Go printer via JS callback.
globalThis.pgfmtParsePlPgSQL = (sql) => {
  try {
    const result = pgQuery.parsePlpgsql(sql);
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

var go = new globalThis.Go();
var result = await WebAssembly.instantiateStreaming(
  fetch("pgfmt.wasm"),
  go.importObject,
);
go.run(result.instance);

// biome-ignore lint/suspicious/noGlobalAssign: we want it
onmessage = (e) => {
  var id = e.data.id;
  try {
    const parsed = pgQuery.parse(e.data.sql);
    if (parsed.error) {
      postMessage({ type: "result", id: id, error: parsed.error.message });
      return;
    }
    const printed = pgfmtPrintParseResult(JSON.stringify(parsed.parse_tree));
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
