// Web Worker: pg-query-emscripten parses SQL, Go WASM prints it.
import pgQueryInit from "https://esm.sh/pg-query-emscripten@5.1.0";

// Load Go WASM runtime (sets globalThis.Go).
import "./wasm_exec.js";

let pgQuery;
let printerReady = false;

globalThis.onPgfmtReady = () => {
  printerReady = true;
  if (pgQuery) postMessage({ type: "ready", version: globalThis.pgfmtVersion });
};

// Forward warnings from Go printer to console.
globalThis.onPgfmtWarn = (msg) => console.warn("[pgfmt]", msg);

// Expose parsing to Go via JS callbacks.
globalThis.pgfmtParse = (sql) => {
  if (sql.length > 100000) {
    return { error: "statement too large for browser parsing" };
  }
  try {
    const result = pgQuery.parse(sql);
    if (result.error) {
      return { error: result.error.message };
    }
    return { result: camelToSnakeKeys(JSON.stringify(result.parse_tree)) };
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

// PL/pgSQL body parsing is disabled in the playground. Emscripten's
// setjmp/longjmp handling corrupts state after repeated parsePlpgsql
// calls, causing hangs or crashes on schemas with many functions.
// Function signatures are still formatted; only $$ bodies pass through.
globalThis.pgfmtParsePlPgSQL = () => {
  return { error: "plpgsql body formatting disabled in playground" };
};

// Convert camelCase JSON keys to snake_case. Only touches keys (before `:`)
// not values. Needed because pg-query-emscripten outputs camelCase but
// protojson requires snake_case for regular proto fields.
function camelToSnakeKeys(json) {
  return json.replace(/"([a-z][a-zA-Z0-9]*)"(\s*:)/g, (_, key, colon) => {
    const snake = key.replace(/[A-Z]/g, (c) => "_" + c.toLowerCase());
    return `"${snake}"${colon}`;
  });
}

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

// Split SQL on top-level semicolons (handles strings, dollar-quotes, comments).
function splitStatements(sql) {
  const stmts = [];
  let start = 0;
  let i = 0;
  while (i < sql.length) {
    const ch = sql[i];
    if (ch === "-" && sql[i + 1] === "-") {
      i += 2;
      while (i < sql.length && sql[i] !== "\n") i++;
    } else if (ch === "/" && sql[i + 1] === "*") {
      i += 2;
      while (i + 1 < sql.length && !(sql[i] === "*" && sql[i + 1] === "/"))
        i++;
      if (i + 1 < sql.length) i += 2;
    } else if (ch === "'") {
      i++;
      while (i < sql.length) {
        if (sql[i] === "'") {
          if (sql[i + 1] === "'") i += 2;
          else { i++; break; }
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
      const stmt = sql.substring(start, i + 1).trim();
      if (stmt && stmt !== ";") stmts.push(sql.substring(start, i + 1));
      start = i + 1;
      i++;
    } else i++;
  }
  if (start < sql.length) {
    const trailing = sql.substring(start).trim();
    if (trailing) stmts.push(sql.substring(start));
  }
  return stmts;
}

onmessage = (e) => {
  const { id, sql } = e.data;
  try {
    // For small inputs, format directly (single Go call handles everything).
    const stmts = splitStatements(sql);
    if (stmts.length <= 1) {
      const result = pgfmtFormat(sql);
      if (result === undefined) {
        postMessage({ type: "result", id, error: "printer crashed" });
        return;
      }
      postMessage({ type: "result", id, result: result.result, error: result.error });
      return;
    }

    // Try parsing the entire input at once — one Emscripten call, one Go call.
    let fullParseFailed = false;
    try {
      const parsed = pgQuery.parse(sql);
      if (parsed && !parsed.error) {
        const scanned = pgfmtScan(sql);
        // pg-query-emscripten outputs camelCase keys for non-node fields
        // (e.g. stmtLocation, stmtLen, returnType) but protojson silently
        // drops camelCase for regular proto fields. Convert to snake_case.
        const parseJSON = camelToSnakeKeys(JSON.stringify(parsed.parse_tree));
        const result = pgfmtFormatParsed(parseJSON, scanned.result, sql);
        if (result && !result.error) {
          postMessage({ type: "result", id, result: result.result });
          return;
        }
      }
      fullParseFailed = true;
    } catch {
      fullParseFailed = true;
    }

    // Fallback: format each statement individually via pgfmtFormat.
    if (fullParseFailed) {
      const parts = [];
      const batchSize = 10;
      function formatBatch(start) {
        const end = Math.min(start + batchSize, stmts.length);
        for (let i = start; i < end; i++) {
          try {
            const r = pgfmtFormat(stmts[i]);
            parts.push(r && !r.error ? r.result : stmts[i].trim() + "\n\n");
          } catch {
            parts.push(stmts[i].trim() + "\n\n");
          }
        }
        if (end < stmts.length) {
          postMessage({ type: "progress", current: end, total: stmts.length });
          setTimeout(() => formatBatch(end), 0);
        } else {
          postMessage({ type: "result", id, result: parts.join("") });
        }
      }
      formatBatch(0);
    }
  } catch (err) {
    postMessage({ type: "result", id, error: err.toString() });
  }
};
