// Web Worker that loads pg-query-emscripten for parsing and TinyGo WASM for printing.
importScripts("wasm_exec.js");

var pgQuery;
var printerReady = false;

// Called by TinyGo WASM when the printer is loaded.
onPgfmtReady = function () {
  printerReady = true;
  checkReady();
};

function checkReady() {
  if (pgQuery && printerReady) {
    postMessage({ type: "ready" });
  }
}

// Initialize pg-query-emscripten.
(async function () {
  importScripts("pg_query_lib.js");
  pgQuery = await new pgQuery();
  checkReady();
})();

// Load TinyGo WASM printer.
(async function () {
  const go = new Go();
  const result = await WebAssembly.instantiateStreaming(
    fetch("pgfmt.wasm"),
    go.importObject
  );
  go.run(result.instance);
})();

// Expose PL/pgSQL parsing to Go via JS callback.
pgfmtParsePlPgSQL = function (sql) {
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

// Split SQL on top-level semicolons, handling strings, dollar-quotes, comments.
function splitStatements(sql) {
  var stmts = [];
  var start = 0;
  var i = 0;
  while (i < sql.length) {
    var ch = sql[i];
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
          if (sql[i + 1] === "'") {
            i += 2;
          } else {
            i++;
            break;
          }
        } else {
          i++;
        }
      }
    } else if (ch === "$") {
      var j = i + 1;
      while (j < sql.length && /[a-zA-Z0-9_]/.test(sql[j])) j++;
      if (j < sql.length && sql[j] === "$") {
        var tag = sql.substring(i, j + 1);
        i = j + 1;
        var end = sql.indexOf(tag, i);
        i = end >= 0 ? end + tag.length : sql.length;
      } else {
        i++;
      }
    } else if (ch === ";") {
      var stmt = sql.substring(start, i + 1).trim();
      if (stmt && stmt !== ";") stmts.push(sql.substring(start, i + 1));
      start = i + 1;
      i++;
    } else {
      i++;
    }
  }
  if (start < sql.length) {
    var trailing = sql.substring(start).trim();
    if (trailing) stmts.push(sql.substring(start));
  }
  return stmts;
}

onmessage = function (e) {
  var id = e.data.id;
  var sql = e.data.sql;
  try {
    var stmts = splitStatements(sql);
    var batchSize = 50;

    function formatBatch(start, parts) {
      var end = Math.min(start + batchSize, stmts.length);
      for (var i = start; i < end; i++) {
        // Parse with pg-query-emscripten.
        var parsed = pgQuery.parse(stmts[i]);
        if (parsed.error) {
          postMessage({
            type: "result",
            id: id,
            error: parsed.error.message || String(parsed.error),
          });
          return;
        }

        // Print with Go WASM. Pass the parse tree as JSON string.
        var parseJSON = JSON.stringify(parsed.parse_tree);
        var printed = pgfmtPrintParseResult(parseJSON);
        if (printed === undefined) {
          postMessage({
            type: "result",
            id: id,
            error: "printer crashed",
          });
          return;
        }
        if (printed.error) {
          postMessage({ type: "result", id: id, error: printed.error });
          return;
        }
        parts.push(printed.result);
      }
      if (end < stmts.length) {
        setTimeout(function () {
          formatBatch(end, parts);
        }, 0);
      } else {
        postMessage({ type: "result", id: id, result: parts.join("") });
      }
    }

    formatBatch(0, []);
  } catch (err) {
    postMessage({ type: "result", id: id, error: err.toString() });
  }
};
