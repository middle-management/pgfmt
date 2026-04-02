// Web Worker: pg-query-emscripten parses SQL, Go WASM prints it.
import pgQueryInit from "https://esm.sh/pg-query-emscripten@5.1.0";

// Load Go WASM runtime (sets globalThis.Go).
import "./wasm_exec.js";

let pgQuery;
let printerReady = false;

globalThis.onPgfmtReady = () => {
  printerReady = true;
  if (pgQuery)
    postMessage({
      type: "ready",
      version: globalThis.pgfmtVersion,
      buildInfo: globalThis.pgfmtBuildInfo,
    });
};

// Forward warnings from Go printer to console.
globalThis.onPgfmtWarn = (msg) => console.warn("[pgfmt]", msg);

// Expose parsing to Go via JS callbacks.
// Track parse call count to detect Emscripten degradation.
let parseCallCount = 0;
const MAX_PARSE_CALLS = 200;

globalThis.pgfmtParse = (sql) => {
  parseCallCount++;
  if (parseCallCount > MAX_PARSE_CALLS) {
    return { error: "too many parse calls; Emscripten state may be degraded" };
  }
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

// PL/pgSQL parsing uses the shared Emscripten instance. Subject to the
// same MAX_PARSE_CALLS limit as pgfmtParse.
globalThis.pgfmtParsePlPgSQL = (sql) => {
  parseCallCount++;
  if (parseCallCount > MAX_PARSE_CALLS) {
    return { error: "parse call limit reached" };
  }
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

// Convert camelCase JSON keys to snake_case for protojson compatibility.
function camelToSnakeKeys(json) {
  return json.replace(/"([a-z][a-zA-Z0-9]*)"(\s*:)/g, (_, key, colon) => {
    const snake = key.replace(/[A-Z]/g, (c) => "_" + c.toLowerCase());
    return `"${snake}"${colon}`;
  });
}

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

// Extract $$ body from a SQL statement (e.g. CREATE FUNCTION ... AS $$ body $$).
function extractBody(sql) {
  const match = sql.match(/\$(\w*)\$([\s\S]*?)\$\1\$/);
  return match ? match[2] : null;
}

// Pre-parse function bodies for a statement. Returns a bodies object
// or null if no bodies found. The Go printer checks these before
// calling back to JS for pgParse/pgParsePlPgSqlToJSON.
function preParseBodies(sql) {
  const body = extractBody(sql);
  if (!body) return null;

  const bodies = { sql: {}, plpgsql: {} };
  const isPlpgsql = /LANGUAGE\s+plpgsql/i.test(sql);
  const isSql = /LANGUAGE\s+sql/i.test(sql);

  if (isPlpgsql) {
    // Try both wrapper prefixes (same as Go's formatPLpgSQLBody).
    const wrappers = [
      "CREATE FUNCTION _plpgsql_fmt_() RETURNS void AS $$",
      "CREATE FUNCTION _plpgsql_fmt_() RETURNS SETOF record AS $$",
    ];
    for (const prefix of wrappers) {
      const wrapped = prefix + body + "\n$$ LANGUAGE plpgsql;";
      parseCallCount++;
      if (parseCallCount > MAX_PARSE_CALLS) break;
      try {
        const result = pgQuery.parsePlpgsql(wrapped);
        if (!result.error) {
          bodies.plpgsql[wrapped] = JSON.stringify(result.plpgsql_funcs);
          break;
        }
      } catch {
        // try next wrapper
      }
    }
  } else if (isSql) {
    parseCallCount++;
    if (parseCallCount <= MAX_PARSE_CALLS) {
      try {
        const result = pgQuery.parse(body);
        if (result && !result.error) {
          bodies.sql[body] = camelToSnakeKeys(
            JSON.stringify(result.parse_tree),
          );
        }
      } catch {
        // body stays unformatted
      }
    }
  }

  return Object.keys(bodies.sql).length || Object.keys(bodies.plpgsql).length
    ? bodies
    : null;
}

// Parse a single statement in JS via pg-query-emscripten.
// Returns { parseJSON, scanJSON, bodiesJSON } or null on failure.
function parseStatement(sql) {
  parseCallCount++;
  if (parseCallCount > MAX_PARSE_CALLS) return null;
  try {
    const parsed = pgQuery.parse(sql);
    if (!parsed || parsed.error) return null;
    const parseJSON = camelToSnakeKeys(JSON.stringify(parsed.parse_tree));
    const scanned = pgfmtScan(sql);
    if (scanned.error) return null;
    const bodies = preParseBodies(sql);
    return {
      parseJSON,
      scanJSON: scanned.result,
      sql,
      bodiesJSON: bodies ? JSON.stringify(bodies) : undefined,
    };
  } catch {
    return null;
  }
}

// Load pg-query-emscripten.
pgQuery = await new pgQueryInit();
if (printerReady)
  postMessage({
    type: "ready",
    version: globalThis.pgfmtVersion,
    buildInfo: globalThis.pgfmtBuildInfo,
  });

// Load Go WASM printer.
const go = new globalThis.Go();
const { instance } = await WebAssembly.instantiateStreaming(
  fetch("pgfmt.wasm"),
  go.importObject,
);
go.run(instance);

onmessage = (e) => {
  const { id, sql } = e.data;
  parseCallCount = 0; // Reset per format request.
  try {
    // For small inputs, use pgfmtFormat (Go handles everything via callbacks).
    const stmts = splitStatements(sql);
    if (stmts.length <= 1) {
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
      return;
    }

    // For large inputs: parse each statement in JS, then format in Go
    // via pgfmtFormatParsed (no Go↔JS callbacks needed per statement).
    const parts = [];
    const batchSize = 20;
    function formatBatch(start) {
      const end = Math.min(start + batchSize, stmts.length);
      for (let i = start; i < end; i++) {
        const parsed = parseStatement(stmts[i]);
        if (parsed) {
          try {
            const result = pgfmtFormatParsed(
              parsed.parseJSON,
              parsed.scanJSON,
              parsed.sql,
              parsed.bodiesJSON,
            );
            if (result && !result.error) {
              parts.push(result.result);
              continue;
            }
          } catch {
            // fall through to raw text
          }
        }
        // Fallback: raw text
        parts.push(stmts[i].trim() + "\n\n");
      }
      if (end < stmts.length) {
        postMessage({ type: "progress", current: end, total: stmts.length });
        setTimeout(() => formatBatch(end), 0);
      } else {
        postMessage({ type: "result", id, result: parts.join("") });
      }
    }
    formatBatch(0);
  } catch (err) {
    postMessage({ type: "result", id, error: err.toString() });
  }
};
