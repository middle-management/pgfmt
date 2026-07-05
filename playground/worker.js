// Web Worker: pg-query-emscripten parses SQL, Go WASI WASM prints it.
// The WASI shim is vendored (see vendor/browser_wasi_shim/) so the playground
// has no runtime dependency on a third-party CDN.
import {
  File,
  OpenFile,
  WASI,
  WASIProcExit,
} from "./vendor/browser_wasi_shim/index.js";
import pgQueryInit from "./pg_query.js";

// WebKit (Safari, iOS Safari) throws "TypeError: Type error" when
// TextDecoder.decode is given a view backed by a resizable ArrayBuffer.
// Recent Emscripten glue uses wasmMemory.toResizableBuffer() when the engine
// supports it, so every string coming out of pg-query-emscripten hits this
// on WebKit. Retry with a copy into a fresh (non-resizable) buffer.
const origDecode = TextDecoder.prototype.decode;
TextDecoder.prototype.decode = function (input, options) {
  try {
    return origDecode.call(this, input, options);
  } catch (err) {
    if (input && ArrayBuffer.isView(input)) {
      const copy = input.buffer.slice(
        input.byteOffset,
        input.byteOffset + input.byteLength,
      );
      return origDecode.call(this, copy, options);
    }
    throw err;
  }
};

let pgQuery;
let wasmModule; // Compiled WebAssembly.Module (compiled once, instantiated per call)

// Track parse call count to detect Emscripten degradation.
let parseCallCount = 0;
const MAX_PARSE_CALLS = 2000;

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
      while (i + 1 < sql.length && !(sql[i] === "*" && sql[i + 1] === "/")) i++;
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

// Split input into SQL text and psql meta-command lines (e.g. \restrict,
// \unrestrict and \connect in pg_dump output). Mirrors Go's splitMetaCommands:
// a line whose first non-blank character is a backslash is a meta-command,
// but only outside strings, quoted identifiers, comments and dollar quotes.
function splitMetaCommands(sql) {
  if (sql.indexOf("\\") === -1) return [{ text: sql }];

  const isIdentStart = (c) => /[a-zA-Z_\u0080-\uffff]/.test(c);
  const isIdent = (c) => /[a-zA-Z0-9_\u0080-\uffff]/.test(c);

  const segs = [];
  let state = "normal";
  let depth = 0; // block comment nesting
  let tag = ""; // dollar-quote delimiter, including both $
  let escapes = false; // current string is E'...' (backslash escapes)
  let segStart = 0;
  let atLineStart = true;

  let i = 0;
  while (i < sql.length) {
    const c = sql[i];
    if (state === "normal") {
      if (c === "\\" && atLineStart) {
        const nl = sql.indexOf("\n", i);
        const lineEnd = nl === -1 ? sql.length : nl + 1;
        if (segStart < i) segs.push({ text: sql.substring(segStart, i) });
        segs.push({ meta: true, text: sql.substring(i, lineEnd) });
        segStart = lineEnd;
        i = lineEnd;
        atLineStart = true;
        continue;
      } else if (c === "'") {
        state = "string";
        escapes =
          i > 0 &&
          (sql[i - 1] === "E" || sql[i - 1] === "e") &&
          (i < 2 || !isIdent(sql[i - 2]));
      } else if (c === '"') {
        state = "ident";
      } else if (c === "-" && sql[i + 1] === "-") {
        state = "lineComment";
        i++;
      } else if (c === "/" && sql[i + 1] === "*") {
        state = "blockComment";
        depth = 1;
        i++;
      } else if (c === "$") {
        let j = i + 1;
        if (j < sql.length && isIdentStart(sql[j])) {
          while (j < sql.length && isIdent(sql[j])) j++;
        }
        if (j < sql.length && sql[j] === "$") {
          state = "dollar";
          tag = sql.substring(i, j + 1);
          i = j + 1;
          atLineStart = false;
          continue;
        }
      }
    } else if (state === "string") {
      if (escapes && c === "\\") {
        i += 2;
        continue;
      }
      if (c === "'") {
        if (sql[i + 1] === "'") {
          i += 2;
          continue;
        }
        state = "normal";
      }
    } else if (state === "ident") {
      if (c === '"') {
        if (sql[i + 1] === '"') {
          i += 2;
          continue;
        }
        state = "normal";
      }
    } else if (state === "lineComment") {
      if (c === "\n") state = "normal";
    } else if (state === "blockComment") {
      if (c === "/" && sql[i + 1] === "*") {
        depth++;
        i++;
      } else if (c === "*" && sql[i + 1] === "/") {
        depth--;
        i++;
        if (depth === 0) state = "normal";
      }
    } else if (state === "dollar") {
      if (sql.startsWith(tag, i)) {
        i += tag.length;
        state = "normal";
        atLineStart = false;
        continue;
      }
    }

    if (c === "\n") atLineStart = true;
    else if (c !== " " && c !== "\t" && c !== "\r") atLineStart = false;
    i++;
  }

  if (segStart < sql.length) segs.push({ text: sql.substring(segStart) });
  if (segs.length === 0) segs.push({ text: sql });
  return segs;
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
      while (i + 1 < sql.length && !(sql[i] === "*" && sql[i + 1] === "/")) i++;
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
  if (parseCallCount > MAX_PARSE_CALLS) {
    lastParseError = `parse limit reached (${parseCallCount}/${MAX_PARSE_CALLS})`;
    console.warn("[pgfmt]", lastParseError);
    return null;
  }
  try {
    const result = pgQuery.parse(sql);
    if (!result || result.error) {
      lastParseError = result?.error?.message || "unknown pg_query error";
      console.warn("[pgfmt] pg_query error:", lastParseError);
      return null;
    }
    return result;
  } catch (e) {
    lastParseError = "pg_query threw: " + String(e);
    console.warn("[pgfmt]", lastParseError);
    return null;
  }
}

function safeDeparse(sql) {
  try {
    const result = pgQuery.format(sql);
    if (!result || result.error) return "";
    return result.query;
  } catch {
    return "";
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
    const stmtJSON = JSON.parse(camelToSnakeKeys(JSON.stringify(rawStmt.stmt)));

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

    // Deparse individual statement for fallback on unsupported node types.
    const stmtText = sql.substring(stmtLocation, stmtEnd).trim();
    const deparsed = safeDeparse(stmtText);

    augmented.stmts.push({
      stmt: stmtJSON,
      stmt_location: stmtLocation,
      stmt_len: stmtLen,
      deparsed,
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
      // Attach stderr (the Go error / panic text) — it is the only real
      // diagnostic, and consoles are hard to reach on mobile browsers.
      const stderr = decoder
        .decode(new Uint8Array(stderrFile.data))
        .trim()
        .slice(0, 500);
      throw new Error(stderr ? String(e.message || e) + " — " + stderr : String(e));
    }
  }

  return decoder.decode(new Uint8Array(stdoutFile.data));
}

// Last per-statement failure reason, surfaced to the UI so fallbacks to raw
// text are visible rather than silent.
let lastFormatError = null;
let lastParseError = null;

// Format a single SQL string via the augmented AST pipeline.
function formatOne(sql) {
  const augmented = buildAugmentedAST(sql);
  if (!augmented) {
    lastFormatError = "parse failed: " + (lastParseError || "unknown");
    console.warn("[pgfmt] parse failed:", sql);
    return null;
  }
  try {
    const t0 = performance.now();
    const result = runWASI(JSON.stringify(augmented));
    const dt = performance.now() - t0;
    if (dt > 500) {
      console.warn(`[pgfmt] slow WASI (${Math.round(dt)}ms):`, sql);
    }
    return result;
  } catch (err) {
    lastFormatError = String(err);
    console.warn("[pgfmt] WASI failed:", err, "sql:", sql);
    return null;
  }
}

// Initialize pg-query-emscripten.
pgQuery = await pgQueryInit();

// Load and compile the WASI WASM module. cache: "no-cache" forces
// revalidation so a cached wasm from an older deploy can never pair with a
// newer worker.js (mismatches make every format call fail).
const wasmResponse = await fetch("pgfmt-print.wasm", { cache: "no-cache" });
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
    // Split out psql meta-command lines first (they are not SQL and cannot
    // be parsed), then split the SQL between them into statements.
    const stmts = [];
    for (const seg of splitMetaCommands(sql)) {
      if (seg.meta) {
        stmts.push({ meta: true, text: seg.text.trim() });
      } else if (seg.text.trim()) {
        for (const s of splitStatements(seg.text)) stmts.push({ text: s });
      }
    }

    // Fast path: format the whole input in a single WASI call. Each call
    // instantiates the module afresh, and memory-constrained browsers
    // (iOS Safari in particular) can fail when that happens once per
    // statement — so prefer one instantiation for the entire input.
    // Bounded so huge documents still get the batched path with progress
    // reporting; on failure, fall through to per-statement formatting to
    // isolate the failing statement.
    if (stmts.length <= 100 && !stmts.some((s) => s.meta)) {
      const result = formatOne(sql);
      if (result !== null) {
        postMessage({ type: "result", id, result });
        return;
      }
      if (stmts.length <= 1) {
        postMessage({
          type: "result",
          id,
          error: "format failed: " + (lastFormatError || "unknown error"),
        });
        return;
      }
    }

    // Large inputs (or fast-path failure): format each statement separately
    // with progress.
    const parts = [];
    const batchSize = 20;
    let failed = 0;
    function formatBatch(start) {
      const end = Math.min(start + batchSize, stmts.length);
      for (let i = start; i < end; i++) {
        if (stmts[i].meta) {
          // psql meta-command: pass through verbatim.
          parts.push(stmts[i].text + "\n\n");
          continue;
        }
        const result = formatOne(stmts[i].text);
        if (result !== null) {
          parts.push(result);
        } else {
          // Fallback: raw text.
          failed++;
          parts.push(stmts[i].text.trim() + "\n\n");
        }
      }
      if (end < stmts.length) {
        postMessage({ type: "progress", current: end, total: stmts.length });
        setTimeout(() => formatBatch(end), 0);
      } else {
        postMessage({
          type: "result",
          id,
          result: parts.join(""),
          failed,
          failReason: failed > 0 ? lastFormatError : undefined,
        });
      }
    }
    formatBatch(0);
  } catch (err) {
    postMessage({ type: "result", id, error: err.toString() });
  }
};
