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

// Split SQL on top-level semicolons, skipping semicolons inside string
// literals, dollar-quoted strings, and comments.
function splitStatements(sql) {
  var stmts = [];
  var start = 0;
  var i = 0;
  while (i < sql.length) {
    var ch = sql[i];
    if (ch === "-" && sql[i + 1] === "-") {
      // single-line comment
      i += 2;
      while (i < sql.length && sql[i] !== "\n") i++;
    } else if (ch === "/" && sql[i + 1] === "*") {
      // block comment
      i += 2;
      while (i + 1 < sql.length && !(sql[i] === "*" && sql[i + 1] === "/"))
        i++;
      if (i + 1 < sql.length) i += 2;
    } else if (ch === "'") {
      // string literal
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
      // dollar-quoted string
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
    if (stmts.length <= 1) {
      var res = pgfmtFormat(sql);
      postMessage({ type: "result", id: id, result: res.result, error: res.error });
      return;
    }
    // Format statements in batches, yielding between batches so the
    // Go GC can run (js/wasm GC can't run during synchronous callbacks).
    var parts = [];
    var batchSize = 50;
    function formatBatch(start) {
      var end = Math.min(start + batchSize, stmts.length);
      for (var i = start; i < end; i++) {
        var res = pgfmtFormat(stmts[i]);
        if (res === undefined) {
          postMessage({ type: "result", id: id, error: "internal error: formatter crashed" });
          return;
        }
        if (res.error) {
          postMessage({ type: "result", id: id, error: res.error });
          return;
        }
        parts.push(res.result);
      }
      if (end < stmts.length) {
        setTimeout(function () { formatBatch(end); }, 0);
      } else {
        postMessage({ type: "result", id: id, result: parts.join("") });
      }
    }
    formatBatch(0);
  } catch (err) {
    postMessage({ type: "result", id: id, error: err.toString() });
  }
};
