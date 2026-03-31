//go:build js && wasm

package printer

import (
	"bytes"
	"container/list"
	"context"
	"embed"
	"encoding/binary"
	"errors"
	"io"
	"runtime"
	"sync"

	pg_query "github.com/pganalyze/pg_query_go/v6"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"google.golang.org/protobuf/proto"
)

//go:embed wasm/libpg_query.wasm
var libPGQueryFS embed.FS

var (
	errFailedWrite = errors.New("failed to write to wasm memory")
	errFailedRead  = errors.New("failed to read from wasm memory")
)

func pgParse(input string) (*pg_query.ParseResult, error) {
	protobufTree, err := parseToProtobuf(input)
	if err != nil {
		return nil, err
	}
	tree := &pg_query.ParseResult{}
	err = proto.Unmarshal(protobufTree, tree)
	return tree, err
}

func pgScan(input string) (*pg_query.ScanResult, error) {
	protobufScan, err := scanToProtobuf(input)
	if err != nil {
		return nil, err
	}
	result := &pg_query.ScanResult{}
	err = proto.Unmarshal(protobufScan, result)
	return result, err
}

func pgParsePlPgSqlToJSON(input string) (string, error) {
	a := getABI()
	defer a.release()

	inputC := a.newCString(input)
	defer inputC.close()

	return a.pgQueryParsePlPgSqlToJSON(inputC)
}

func parseToProtobuf(input string) ([]byte, error) {
	a := getABI()
	defer a.release()

	inputC := a.newCString(input)
	defer inputC.close()

	return a.pgQueryParseProtobuf(inputC)
}

func scanToProtobuf(input string) ([]byte, error) {
	a := getABI()
	defer a.release()

	inputC := a.newCString(input)
	defer inputC.close()

	return a.pgQueryScanProtobuf(inputC)
}

// wazero runtime and compiled module setup

func newRT() (wazero.Runtime, wazero.CompiledModule) {
	ctx := context.Background()

	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().
		WithCompilationCache(wazero.NewCompilationCache()).
		WithCoreFeatures(api.CoreFeaturesV2|experimental.CoreFeaturesThreads))

	wasi_snapshot_preview1.MustInstantiate(ctx, rt)
	mustInstantiateWasix(ctx, rt)

	wasmBytes, err := libPGQueryFS.ReadFile("wasm/libpg_query.wasm")
	if err != nil {
		panic(err)
	}

	code, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		panic(err)
	}

	return rt, code
}

// abi pool

var (
	abiPool   = list.New()
	abiPoolMu sync.Mutex
)

func newABI() *wasmABI {
	ctx := context.Background()
	ctx = experimental.WithMemoryAllocator(ctx, experimental.MemoryAllocatorFunc(func(cap, max uint64) experimental.LinearMemory {
		return &sliceBuffer{buf: make([]byte, 0, cap), max: max}
	}))

	rt, code := newRT()
	// Use io.Discard instead of os.Stdout/os.Stderr to avoid stat /dev/stdout
	// which panics in js/wasm environments.
	cfg := wazero.NewModuleConfig().
		WithSysNanotime().
		WithStdout(io.Discard).
		WithStderr(io.Discard).
		WithStartFunctions("_initialize")
	mod, err := rt.InstantiateModule(ctx, code, cfg)
	if err != nil {
		panic(err)
	}
	res := &wasmABI{
		fPgQueryInit:                    newLazyFunction(mod, "pg_query_init"),
		fPgQueryParseProtobuf:           newLazyFunction(mod, "pg_query_parse_protobuf"),
		fPgQueryFreeProtobufParseResult: newLazyFunction(mod, "pg_query_free_protobuf_parse_result"),
		fPgQueryParsePlpgsql:            newLazyFunction(mod, "pg_query_parse_plpgsql"),
		fPgQueryFreePlpgsqlParseResult:  newLazyFunction(mod, "pg_query_free_plpgsql_parse_result"),
		fPgQueryScan:                    newLazyFunction(mod, "pg_query_scan"),
		fPgQueryFreeScanResult:          newLazyFunction(mod, "pg_query_free_scan_result"),

		malloc: newLazyFunction(mod, "malloc"),
		free:   newLazyFunction(mod, "free"),

		mod:        mod,
		wasmMemory: mod.Memory(),
		rt:         rt,
	}

	res.pgQueryInit()
	runtime.SetFinalizer(res, func(r *wasmABI) {
		r.rt.Close(context.Background())
	})

	return res
}

func getABI() *wasmABI {
	abiPoolMu.Lock()
	e := abiPool.Front()
	if e == nil {
		abiPoolMu.Unlock()
		return newABI()
	}
	abiPool.Remove(e)
	abiPoolMu.Unlock()
	return e.Value.(*wasmABI)
}

type wasmABI struct {
	fPgQueryInit                    lazyFunction
	fPgQueryParseProtobuf           lazyFunction
	fPgQueryFreeProtobufParseResult lazyFunction
	fPgQueryParsePlpgsql            lazyFunction
	fPgQueryFreePlpgsqlParseResult  lazyFunction
	fPgQueryScan                    lazyFunction
	fPgQueryFreeScanResult          lazyFunction

	malloc lazyFunction
	free   lazyFunction

	wasmMemory api.Memory
	mod        api.Module
	rt         wazero.Runtime
}

func (a *wasmABI) release() {
	abiPoolMu.Lock()
	abiPool.PushBack(a)
	abiPoolMu.Unlock()
}

func (a *wasmABI) pgQueryInit() {
	a.fPgQueryInit.call0(context.Background())
}

func (a *wasmABI) pgQueryParseProtobuf(input cString) ([]byte, error) {
	ctx := wasixBackgroundContext()

	resPtr := a.malloc.call1(ctx, 16)
	defer a.free.call1(ctx, resPtr)

	a.fPgQueryParseProtobuf.call2(ctx, resPtr, uint64(input.ptr))
	defer a.fPgQueryFreeProtobufParseResult.call1(ctx, resPtr)

	resBuf, ok := a.wasmMemory.Read(uint32(resPtr), 16)
	if !ok {
		panic(errFailedRead)
	}

	errPtr := binary.LittleEndian.Uint32(resBuf[12:])
	if errPtr != 0 {
		return nil, newPgQueryError(a.mod, errPtr)
	}

	pgQueryProtobufLen := binary.LittleEndian.Uint32(resBuf)
	pgQueryProtobufData := binary.LittleEndian.Uint32(resBuf[4:])

	buf, ok := a.wasmMemory.Read(pgQueryProtobufData, pgQueryProtobufLen)
	if !ok {
		panic(errFailedRead)
	}

	return bytes.Clone(buf), nil
}

func (a *wasmABI) pgQueryScanProtobuf(input cString) ([]byte, error) {
	ctx := wasixBackgroundContext()

	resPtr := a.malloc.call1(ctx, 16)
	defer a.free.call1(ctx, resPtr)

	a.fPgQueryScan.call2(ctx, resPtr, uint64(input.ptr))
	defer a.fPgQueryFreeScanResult.call1(ctx, resPtr)

	resBuf, ok := a.wasmMemory.Read(uint32(resPtr), 16)
	if !ok {
		panic(errFailedRead)
	}

	errPtr := binary.LittleEndian.Uint32(resBuf[12:])
	if errPtr != 0 {
		return nil, newPgQueryError(a.mod, errPtr)
	}

	pgQueryProtobufLen := binary.LittleEndian.Uint32(resBuf)
	pgQueryProtobufData := binary.LittleEndian.Uint32(resBuf[4:])

	buf, ok := a.wasmMemory.Read(pgQueryProtobufData, pgQueryProtobufLen)
	if !ok {
		panic(errFailedRead)
	}

	return bytes.Clone(buf), nil
}

func (a *wasmABI) pgQueryParsePlPgSqlToJSON(input cString) (string, error) {
	ctx := wasixBackgroundContext()

	resPtr := a.malloc.call1(ctx, 8)
	defer a.free.call1(ctx, resPtr)

	a.fPgQueryParsePlpgsql.call2(ctx, resPtr, uint64(input.ptr))
	defer a.fPgQueryFreePlpgsqlParseResult.call1(ctx, resPtr)

	resBuf, ok := a.wasmMemory.Read(uint32(resPtr), 8)
	if !ok {
		panic(errFailedRead)
	}

	errPtr := binary.LittleEndian.Uint32(resBuf[4:])
	if errPtr != 0 {
		return "", newPgQueryError(a.mod, errPtr)
	}

	return readCStringPtr(a.wasmMemory, uint32(resPtr)), nil
}

// cString helpers

type cString struct {
	ptr    uint32
	length int
	a      *wasmABI
}

func (a *wasmABI) newCString(s string) cString {
	ptr := uint32(a.malloc.call1(context.Background(), uint64(len(s)+1)))
	if !a.wasmMemory.WriteString(ptr, s) {
		panic(errFailedWrite)
	}
	if !a.wasmMemory.WriteByte(ptr+uint32(len(s)), 0) {
		panic(errFailedWrite)
	}
	return cString{ptr: ptr, length: len(s), a: a}
}

func (s cString) close() {
	s.a.free.call1(context.Background(), uint64(s.ptr))
}

// lazy function wrapper

type lazyFunction struct {
	f    api.Function
	name string
	mod  api.Module
}

func newLazyFunction(mod api.Module, name string) lazyFunction {
	return lazyFunction{mod: mod, name: name}
}

func (f *lazyFunction) call0(ctx context.Context) uint64 {
	var callStack [1]uint64
	return f.callWithStack(ctx, callStack[:])
}

func (f *lazyFunction) call1(ctx context.Context, arg1 uint64) uint64 {
	var callStack [1]uint64
	callStack[0] = arg1
	return f.callWithStack(ctx, callStack[:])
}

func (f *lazyFunction) call2(ctx context.Context, arg1 uint64, arg2 uint64) uint64 {
	var callStack [2]uint64
	callStack[0] = arg1
	callStack[1] = arg2
	return f.callWithStack(ctx, callStack[:])
}

func (f *lazyFunction) callWithStack(ctx context.Context, callStack []uint64) uint64 {
	if f.f == nil {
		f.f = f.mod.ExportedFunction(f.name)
	}
	if err := f.f.CallWithStack(ctx, callStack); err != nil {
		panic(err)
	}
	return callStack[0]
}

// error and string helpers

func newPgQueryError(mod api.Module, errPtr uint32) error {
	message := readCStringPtr(mod.Memory(), errPtr)
	return errors.New(message)
}

func readCStringPtr(mem api.Memory, ptrptr uint32) string {
	ptr, ok := mem.ReadUint32Le(ptrptr)
	if !ok {
		panic(errFailedRead)
	}
	if ptr == 0 {
		return ""
	}
	endPtr := ptr
	for {
		if b, ok := mem.ReadByte(endPtr); !ok {
			panic(errFailedRead)
		} else if b == 0 {
			break
		}
		endPtr++
	}
	buf, ok := mem.Read(ptr, endPtr-ptr)
	if !ok {
		panic(errFailedRead)
	}
	return string(buf)
}

// sliceBuffer is a growing memory allocator for wazero.
// It starts with a small initial capacity and grows on demand up to max.
type sliceBuffer struct {
	buf []byte
	max uint64
}

func (b *sliceBuffer) Reallocate(size uint64) []byte {
	if size > b.max {
		return nil
	}
	if uint64(cap(b.buf)) >= size {
		b.buf = b.buf[:size]
		return b.buf
	}
	// Grow: allocate a new buffer and copy existing data.
	newCap := uint64(cap(b.buf)) * 2
	if newCap < size {
		newCap = size
	}
	if newCap > b.max {
		newCap = b.max
	}
	newBuf := make([]byte, size, newCap)
	copy(newBuf, b.buf)
	b.buf = newBuf
	return b.buf
}

func (b *sliceBuffer) Free() {}
