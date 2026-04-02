// Web Worker: pg-query-emscripten parses SQL, Go WASI WASM prints it.
import pgQueryInit from "https://esm.sh/pg-query-emscripten@5.1.0";
import { WASI, File, OpenFile, WASIProcExit } from "https://esm.sh/@bjorn3/browser_wasi_shim@0.4.2";

let pgQuery;
let wasmModule; // Compiled WebAssembly.Module (compiled once, instantiated per call)

// Track parse call count to detect Emscripten degradation.
let parseCallCount = 0;
const MAX_PARSE_CALLS = 200;

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

// Pure JS scanner — finds comments and semicolons.
function scanTokens(sql) {
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
  return tokens;
}

// Extract comments from scan tokens.
function extractComments(sql, tokens) {
  return tokens
    .filter((t) => t[2] === "SQL_COMMENT" || t[2] === "C_COMMENT")
    .map((t) => ({ text: sql.substring(t[0], t[1]), start: t[0], end: t[1] }));
}

// Extract $$ body from a SQL statement.
function extractBody(sql) {
  const match = sql.match(/\$(\w*)\$([\s\S]*?)\$\1\$/);
  return match ? match[2] : null;
}

// Guarded parse — returns null if Emscripten call limit reached.
function safeParse(sql) {
  parseCallCount++;
  if (parseCallCount > MAX_PARSE_CALLS) return null;
  try {
    const result = pgQuery.parse(sql);
    return result && !result.error ? result : null;
  } catch {
    return null;
  }
}

function safeParsePlpgsql(sql) {
  parseCallCount++;
  if (parseCallCount > MAX_PARSE_CALLS) return null;
  try {
    const result = pgQuery.parsePlpgsql(sql);
    return result && !result.error ? result : null;
  } catch {
    return null;
  }
}

// Build augmented AST JSON for a single SQL statement.
// This is the JS equivalent of Go's printer.Augment().
function buildAugmentedAST(sql) {
  const parsed = safeParse(sql);
  if (!parsed) return null;

  const parseTree = parsed.parse_tree;
  const tokens = scanTokens(sql);
  const comments = extractComments(sql, tokens);

  const augmented = { version: parseTree.version || 0, stmts: [] };
  let ci = 0;

  for (const rawStmt of parseTree.stmts || []) {
    const stmtLocation = rawStmt.stmt_location || 0;
    const stmtLen = rawStmt.stmt_len || 0;
    const stmtEnd = stmtLen > 0 ? stmtLocation + stmtLen : sql.length;

    // Find first real (non-comment) token in the statement range.
    let realStart = stmtEnd;
    for (const tok of tokens) {
      if (tok[0] < stmtLocation) continue;
      if (tok[0] >= stmtEnd) break;
      if (tok[2] !== "SQL_COMMENT" && tok[2] !== "C_COMMENT") {
        realStart = tok[0];
        break;
      }
    }

    // Emit leading comments.
    while (ci < comments.length && comments[ci].start < realStart) {
      const c = comments[ci];
      augmented.stmts.push({
        comment: {
          text: c.text,
          type: c.text.startsWith("/*") ? "block" : "line",
        },
      });
      ci++;
    }

    // Collect inline comments for this statement.
    const inlineComments = [];
    while (ci < comments.length && comments[ci].start < stmtEnd) {
      inlineComments.push(comments[ci]);
      ci++;
    }

    // Build the statement JSON (snake_case for protojson compatibility).
    let stmtJSON = JSON.parse(camelToSnakeKeys(JSON.stringify(rawStmt.stmt)));

    // Embed inline comments as _comments sidecar.
    if (inlineComments.length > 0) {
      stmtJSON._comments = inlineComments.map((c) => ({
        text: c.text,
        start: c.start,
        end: c.end,
      }));
    }

    // Pre-parse function bodies.
    const bodies = preParseBodiesForStmt(sql, rawStmt);
    if (bodies) {
      stmtJSON._bodies = bodies;
    }

    augmented.stmts.push({
      stmt: stmtJSON,
      stmt_location: stmtLocation,
      stmt_len: stmtLen,
    });
  }

  // Trailing comments.
  while (ci < comments.length) {
    const c = comments[ci];
    augmented.stmts.push({
      comment: {
        text: c.text,
        type: c.text.startsWith("/*") ? "block" : "line",
      },
    });
    ci++;
  }

  return augmented;
}

// Pre-parse function bodies for a statement.
function preParseBodiesForStmt(sql, rawStmt) {
  const body = extractBody(sql);
  if (!body) return null;

  const isPlpgsql = /LANGUAGE\s+plpgsql/i.test(sql);
  const isSql = /LANGUAGE\s+sql/i.test(sql);
  // DO blocks default to plpgsql
  const isDo = rawStmt.stmt?.DoStmt || rawStmt.stmt?.do_stmt;
  const lang = isPlpgsql || (isDo && !isSql) ? "plpgsql" : isSql ? "sql" : "";
  if (!lang) return null;

  const bodies = { sql: {}, plpgsql: {} };

  if (lang === "sql") {
    try {
      const result = safeParse(body);
      if (result && !result.error) {
        // Marshal as ParseResult-like JSON for Go's protojson.Unmarshal.
        const stmts = (result.parse_tree.stmts || []).map((s) => ({
          stmt: s.stmt,
        }));
        bodies.sql[body] = camelToSnakeKeys(
          JSON.stringify({ version: result.parse_tree.version, stmts }),
        );
      }
    } catch {
      // body stays unformatted
    }
  } else if (lang === "plpgsql") {
    const wrappers = [
      "CREATE FUNCTION _plpgsql_fmt_() RETURNS void AS $$",
      "CREATE FUNCTION _plpgsql_fmt_() RETURNS SETOF record AS $$",
    ];
    for (const prefix of wrappers) {
      const wrapped = prefix + body + "\n$$ LANGUAGE plpgsql;";
      try {
        const result = safeParsePlpgsql(wrapped);
        if (result) {
          bodies.plpgsql[wrapped] = JSON.stringify(result.plpgsql_funcs);
          // Also pre-parse embedded SQL within PL/pgSQL bodies.
          preParsePlpgsqlEmbeddedSQL(result.plpgsql_funcs, bodies);
          break;
        }
      } catch {
        // try next wrapper
      }
    }
  }

  return Object.keys(bodies.sql).length || Object.keys(bodies.plpgsql).length
    ? bodies
    : null;
}

// Pre-parse SQL queries embedded within PL/pgSQL bodies.
function preParsePlpgsqlEmbeddedSQL(plpgsqlFuncs, bodies) {
  const sqlStrings = new Set();
  JSON.stringify(plpgsqlFuncs, (key, value) => {
    if (
      (key === "query" || key === "sqlstmt") &&
      typeof value === "string" &&
      value.trim()
    ) {
      sqlStrings.add(value);
    }
    return value;
  });
  for (const sql of sqlStrings) {
    if (bodies.sql[sql]) continue;
    const result = safeParse(sql);
    if (result) {
      const stmts = (result.parse_tree.stmts || []).map((s) => ({
        stmt: s.stmt,
      }));
      bodies.sql[sql] = camelToSnakeKeys(
        JSON.stringify({ version: result.parse_tree.version, stmts }),
      );
    }
  }
}

// Run the WASI WASM binary with the given stdin data.
// Returns the stdout output as a string.
function runWASI(stdinData) {
  const encoder = new TextEncoder();
  const decoder = new TextDecoder();

  const stdinFile = new File(encoder.encode(stdinData));
  const stdoutFile = new File([]);
  const stderrFile = new File([]);

  const wasi = new WASI(
    ["pgfmt-print"],
    [],
    [
      new OpenFile(stdinFile),
      new OpenFile(stdoutFile),
      new OpenFile(stderrFile),
    ],
    { debug: false },
  );

  const instance = new WebAssembly.Instance(wasmModule, {
    wasi_snapshot_preview1: wasi.wasiImport,
  });

  try {
    wasi.start(instance);
  } catch (e) {
    // Go's os.Exit(0) triggers WASIProcExit with code 0.
    if (e instanceof WASIProcExit && e.code === 0) {
      // Normal exit.
    } else {
      throw e;
    }
  }

  return decoder.decode(new Uint8Array(stdoutFile.data));
}

// Format a single SQL string via the augmented AST pipeline.
function formatOne(sql) {
  const augmented = buildAugmentedAST(sql);
  if (!augmented) {
    console.warn("[pgfmt] parse failed:", sql.slice(0, 80));
    return null;
  }
  try {
    return runWASI(JSON.stringify(augmented));
  } catch (err) {
    console.warn("[pgfmt] WASI execution failed:", err, "sql:", sql.slice(0, 80));
    return null;
  }
}

// Initialize pg-query-emscripten.
pgQuery = await new pgQueryInit();

// Load and compile the WASI WASM module.
const wasmResponse = await fetch("pgfmt-print.wasm");
wasmModule = await WebAssembly.compile(await wasmResponse.arrayBuffer());

// Signal ready.
postMessage({
  type: "ready",
  version: "pgfmt",
  buildInfo: "pgfmt WASI",
});

onmessage = (e) => {
  const { id, sql } = e.data;
  parseCallCount = 0; // Reset per format request.
  try {
    const stmts = splitStatements(sql);

    if (stmts.length <= 1) {
      const result = formatOne(sql);
      if (result !== null) {
        postMessage({ type: "result", id, result });
      } else {
        postMessage({ type: "result", id, error: "format failed" });
      }
      return;
    }

    // Large inputs: format each statement separately with progress.
    const parts = [];
    const batchSize = 20;
    function formatBatch(start) {
      const end = Math.min(start + batchSize, stmts.length);
      for (let i = start; i < end; i++) {
        const result = formatOne(stmts[i]);
        if (result !== null) {
          parts.push(result);
        } else {
          // Fallback: raw text.
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
  } catch (err) {
    postMessage({ type: "result", id, error: err.toString() });
  }
};
