// Web Worker: pg-query-emscripten parses SQL, Go WASM prints it.
importScripts("wasm_exec.js");

var pgQuery;
var printerReady = false;

onPgfmtReady = function () {
  printerReady = true;
  if (pgQuery) postMessage({ type: "ready" });
};

// Expose PL/pgSQL parsing to Go printer via JS callback.
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

// Load pg-query-emscripten (Emscripten-compiled libpg_query).
(async function () {
  importScripts("pg_query_lib.js");
  pgQuery = await new pgQuery();
  if (printerReady) postMessage({ type: "ready" });
})();

// Load Go WASM (printer only).
(async function () {
  var go = new Go();
  var result = await WebAssembly.instantiateStreaming(
    fetch("pgfmt.wasm"),
    go.importObject
  );
  go.run(result.instance);
})();

onmessage = function (e) {
  var id = e.data.id;
  try {
    // Parse SQL with pg-query-emscripten.
    var parsed = pgQuery.parse(e.data.sql);
    if (parsed.error) {
      postMessage({ type: "result", id: id, error: parsed.error.message });
      return;
    }

    // Format with Go WASM printer.
    var printed = pgfmtPrintParseResult(JSON.stringify(parsed.parse_tree));
    if (printed === undefined) {
      postMessage({ type: "result", id: id, error: "printer crashed" });
      return;
    }
    postMessage({ type: "result", id: id, result: printed.result, error: printed.error });
  } catch (err) {
    postMessage({ type: "result", id: id, error: err.toString() });
  }
};
