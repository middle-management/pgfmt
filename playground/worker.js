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

// Pure JS scanner — finds comments and semicolons without calling Emscripten.
// pg-query-emscripten's scan() uses ALLOC_STACK which overflows on large inputs,
// and returns Emscripten vectors that crash when accessed.
globalThis.pgfmtScan = (sql) => {
  try {
    const tokens = [];
    let i = 0;
    while (i < sql.length) {
      const ch = sql[i];
      if (ch === "-" && sql[i + 1] === "-") {
        const start = i;
        i += 2;
        while (i < sql.length && sql[i] !== "\n") i++;
        tokens.push([start, i, "SQL_COMMENT", "NO_KEYWORD"]);
      } else if (ch === "/" && sql[i + 1] === "*") {
        const start = i;
        i += 2;
        while (i + 1 < sql.length && !(sql[i] === "*" && sql[i + 1] === "/"))
          i++;
        if (i + 1 < sql.length) i += 2;
        tokens.push([start, i, "C_COMMENT", "NO_KEYWORD"]);
      } else if (ch === "'") {
        i++;
        while (i < sql.length) {
          if (sql[i] === "'") {
            if (sql[i + 1] === "'") i += 2;
            else {
              i++;
              break;
            }
          } else i++;
        }
      } else if (ch === "$") {
        let j = i + 1;
        while (j < sql.length && /[a-zA-Z0-9_]/.test(sql[j])) j++;
        if (j < sql.length && sql[j] === "$") {
          const tag = sql.substring(i, j + 1);
          i = j + 1;
          const end = sql.indexOf(tag, i);
          i = end >= 0 ? end + tag.length : sql.length;
        } else i++;
      } else if (ch === ";") {
        tokens.push([i, i + 1, "ASCII_59", "NO_KEYWORD"]);
        i++;
      } else {
        i++;
      }
    }
    return { result: JSON.stringify(tokens) };
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
    postMessage({
      type: "result",
      id,
      result: result.result,
      error: result.error,
    });
  } catch (err) {
    postMessage({ type: "result", id, error: err.toString() });
  }
};
