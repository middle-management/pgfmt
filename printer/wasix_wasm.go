//go:build js && wasm

package printer

// WASIX module implementation for wazero, adapted from
// github.com/wasilibs/go-pgquery/internal/wasix_32v1.

import (
	"context"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
)

const wasixModuleName = "wasix_32v1"

const i32, i64 = api.ValueTypeI32, api.ValueTypeI64

func mustInstantiateWasix(ctx context.Context, r wazero.Runtime) {
	builder := r.NewHostModuleBuilder(wasixModuleName)
	exportWasixFunctions(builder)
	if _, err := builder.Instantiate(ctx); err != nil {
		panic(err)
	}
}

func exportWasixFunctions(builder wazero.HostModuleBuilder) {
	builder.NewFunctionBuilder().
		WithGoModuleFunction(callbackSignalFn, []api.ValueType{i32, i32}, []api.ValueType{}).
		Export("callback_signal")

	builder.NewFunctionBuilder().
		WithGoModuleFunction(futexWaitFn, []api.ValueType{i32, i32, i32, i32}, []api.ValueType{i32}).
		Export("futex_wait")

	builder.NewFunctionBuilder().
		WithGoModuleFunction(futexWakeFn, []api.ValueType{i32, i32}, []api.ValueType{i32}).
		Export("futex_wake")

	builder.NewFunctionBuilder().
		WithGoModuleFunction(futexWakeAllFn, []api.ValueType{i32, i32}, []api.ValueType{i32}).
		Export("futex_wake_all")

	builder.NewFunctionBuilder().
		WithGoModuleFunction(stackCheckpointFn, []api.ValueType{i32, i32}, []api.ValueType{i32}).
		Export("stack_checkpoint")

	builder.NewFunctionBuilder().
		WithGoModuleFunction(stackRestoreFn, []api.ValueType{i32, i64}, []api.ValueType{}).
		Export("stack_restore")

	builder.NewFunctionBuilder().
		WithGoModuleFunction(procIDFn, []api.ValueType{i32}, []api.ValueType{i32}).
		Export("proc_id")

	builder.NewFunctionBuilder().
		WithGoModuleFunction(threadExitFn, []api.ValueType{i32}, []api.ValueType{}).
		Export("thread_exit")

	builder.NewFunctionBuilder().
		WithGoModuleFunction(threadSignalFn, []api.ValueType{i32, i32}, []api.ValueType{i32}).
		Export("thread_signal")
}

var callbackSignalFn = api.GoModuleFunc(func(_ context.Context, _ api.Module, _ []uint64) {
	panic("callback_signal")
})

var futexWaitFn = api.GoModuleFunc(func(_ context.Context, _ api.Module, _ []uint64) {
	panic("futex_wait")
})

var futexWakeFn = api.GoModuleFunc(func(_ context.Context, _ api.Module, _ []uint64) {
	panic("futex_wake")
})

var futexWakeAllFn = api.GoModuleFunc(func(_ context.Context, _ api.Module, _ []uint64) {
	panic("futex_wake_all")
})

var procIDFn = api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) {
	resPtr := uint32(stack[0])
	m.Memory().WriteUint32Le(resPtr, 1)
	stack[0] = 0
})

var stackCheckpointFn = api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
	snapshotPtr := stack[0]
	retValPtr := stack[1]
	d := ctx.Value(wasixDataKey{}).(*wasixData)

	cstackPointer := uint32(mod.ExportedGlobal("__stack_pointer").Get())
	cstackTop := uint32(mod.ExportedGlobal("__heap_base").Get())
	cstackView, ok := mod.Memory().Read(cstackPointer, cstackTop-cstackPointer)
	if !ok {
		panic("read failed")
	}
	cstack := make([]byte, len(cstackView))
	copy(cstack, cstackView)

	sc := experimental.GetSnapshotter(ctx)
	s := sc.Snapshot()

	idx := len(d.checkpoints)
	d.checkpoints = append(d.checkpoints, wasixCheckpoint{
		snapshot:      s,
		retValPtr:     uint32(retValPtr),
		cstackPointer: cstackPointer,
		cstack:        cstack,
	})

	if !mod.Memory().WriteUint64Le(uint32(snapshotPtr), uint64(idx)) {
		panic("write failed")
	}
	if !mod.Memory().WriteUint64Le(uint32(retValPtr), 0) {
		panic("write failed")
	}

	stack[0] = 0
})

var stackRestoreFn = api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
	snapshotPtr := stack[0]
	ret := stack[1]

	snapshotIdx, ok := mod.Memory().ReadUint64Le(uint32(snapshotPtr))
	if !ok {
		panic("read failed")
	}

	d := ctx.Value(wasixDataKey{}).(*wasixData)
	c := d.checkpoints[snapshotIdx]

	mod.ExportedGlobal("__stack_pointer").(api.MutableGlobal).Set(uint64(c.cstackPointer))
	mod.Memory().Write(c.cstackPointer, c.cstack)

	mod.Memory().WriteUint64Le(c.retValPtr, ret)
	stack[0] = 0
	c.snapshot.Restore(stack[:1])
})

var threadExitFn = api.GoModuleFunc(func(_ context.Context, _ api.Module, _ []uint64) {
	panic("thread_exit")
})

var threadSignalFn = api.GoModuleFunc(func(_ context.Context, _ api.Module, _ []uint64) {
	panic("thread_signal")
})

type wasixCheckpoint struct {
	snapshot      experimental.Snapshot
	retValPtr     uint32
	cstackPointer uint32
	cstack        []byte
}

type wasixData struct {
	checkpoints []wasixCheckpoint
}

type wasixDataKey struct{}

func wasixBackgroundContext() context.Context {
	ctx := context.Background()
	ctx = experimental.WithSnapshotter(ctx)
	ctx = context.WithValue(ctx, wasixDataKey{}, &wasixData{})
	return ctx
}
