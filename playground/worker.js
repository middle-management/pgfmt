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

// Forward warnings from Go printer to console.
globalThis.onPgfmtWarn = (msg) => console.warn("[pgfmt]", msg);

// Expose parsing to Go via JS callbacks.
globalThis.pgfmtParse = (sql) => {
  try {
    const result = pgQuery.parse(sql);
    if (result.error) {
      return { error: result.error.message };
    }
    return { result: JSON.stringify(result.parse_tree) };
  } catch (err) {
    return { error: err.toString() };
  }
};

globalThis.pgfmtScan = (sql) => {
  try {
    const result = pgQuery.scan(sql);
    if (result.error) {
      return { error: result.error.message };
    }
    // pg-query-emscripten returns an Emscripten vector, convert to plain array.
    const tokens = [];
    for (let i = 0; i < result.tokens.size(); i++) {
      tokens.push(result.tokens.get(i));
    }
    return { tokens };
  } catch (err) {
    return { error: err.toString() };
  }
};

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

// Load Go WASM printer.
const go = new globalThis.Go();
const { instance } = await WebAssembly.instantiateStreaming(
  fetch("pgfmt.wasm"),
  go.importObject,
);
go.run(instance);

onmessage = (e) => {
  const { id, sql } = e.data;
  try {
    const result = pgfmtFormat(sql);
    if (result === undefined) {
      postMessage({ type: "result", id, error: "printer crashed" });
      return;
    }
    postMessage({ type: "result", id, result: result.result, error: result.error });
  } catch (err) {
    postMessage({ type: "result", id, error: err.toString() });
  }
};
