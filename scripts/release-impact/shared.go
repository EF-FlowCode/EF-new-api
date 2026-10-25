package main

import (
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// The new Web and the old worker exchange data through MySQL, Redis and
// /data. A changed function the worker never runs may still change what the
// worker reads, so a changed, worker-unreachable function that shapes shared
// data blocks blue-green when it:
//   - sets fields of a type the worker uses (rows, payloads, settings);
//   - builds free-form data (string-keyed maps of any, JSON encoding);
//   - writes rows of a worker-used model, or rows it cannot identify
//     (raw SQL, an Update chain without a resolvable Model);
//   - writes Redis or files at all (keys and paths are not statically known).
// Pure reads and writes of models the worker never uses stay compatible.

var gormWrites = map[string]bool{"Create": true, "CreateInBatches": true, "Save": true, "Update": true,
	"Updates": true, "UpdateColumn": true, "UpdateColumns": true, "Delete": true, "Exec": true,
	"FirstOrCreate": true, "Raw": true}

var gormChain = map[string]bool{"Where": true, "Not": true, "Or": true, "Select": true, "Omit": true,
	"Clauses": true, "Session": true, "WithContext": true, "Unscoped": true, "Limit": true, "Offset": true,
	"Order": true, "Debug": true, "Joins": true, "Preload": true, "Scopes": true, "Attrs": true, "Assign": true}

var redisWrites = map[string]bool{"Set": true, "SetNX": true, "SetEX": true, "SetXX": true, "SetArgs": true,
	"GetSet": true, "GetDel": true, "GetEx": true, "Del": true, "Unlink": true, "Expire": true, "ExpireAt": true,
	"PExpire": true, "PExpireAt": true, "Persist": true, "Incr": true, "IncrBy": true, "IncrByFloat": true,
	"Decr": true, "DecrBy": true, "HSet": true, "HMSet": true, "HSetNX": true, "HDel": true, "HIncrBy": true,
	"HIncrByFloat": true, "LPush": true, "RPush": true, "LPushX": true, "RPushX": true, "LPop": true, "RPop": true,
	"LRem": true, "LSet": true, "LTrim": true, "LInsert": true, "SAdd": true, "SRem": true, "SPop": true,
	"SMove": true, "ZAdd": true, "ZAddNX": true, "ZAddXX": true, "ZRem": true, "ZIncrBy": true,
	"ZRemRangeByScore": true, "ZRemRangeByRank": true, "Publish": true, "Eval": true, "EvalSha": true,
	"Run": true, "Rename": true, "RenameNX": true, "XAdd": true, "XDel": true, "XTrim": true, "Append": true,
	"MSet": true, "MSetNX": true, "SetBit": true, "PFAdd": true, "Pipelined": true, "TxPipelined": true,
	"Exec": true, "FlushDB": true, "FlushAll": true}

var fileWrites = map[string]bool{"WriteFile": true, "Create": true, "OpenFile": true, "Rename": true,
	"Remove": true, "RemoveAll": true, "Mkdir": true, "MkdirAll": true, "Symlink": true, "Link": true,
	"Chmod": true, "Chown": true, "Truncate": true, "CreateTemp": true, "MkdirTemp": true}

type typeLookup func(types.Type) (key string, workerUsed bool)

func sharedDataWriters(prog *ssa.Program, changed []finding, reached map[string]bool, used map[string]bool,
	inModule func(*types.Package) bool) []finding {
	byKey := map[string][]*ssa.Function{}
	for fn := range ssautil.AllFunctions(prog) {
		if fn.Parent() != nil || fn.Synthetic != "" {
			continue
		}
		top := fn
		if origin := fn.Origin(); origin != nil {
			top = origin
		}
		if obj, ok := top.Object().(*types.Func); ok && inModule(obj.Pkg()) {
			if key := funcKey(obj); key != "" {
				byKey[key] = append(byKey[key], fn)
			}
		}
	}
	lookup := func(t types.Type) (string, bool) {
		for t != nil {
			switch u := types.Unalias(t).(type) {
			case *types.Pointer:
				t = u.Elem()
			case *types.Slice:
				t = u.Elem()
			case *types.Array:
				t = u.Elem()
			case *types.Named:
				obj := u.Origin().Obj()
				if !inModule(obj.Pkg()) {
					return "", false
				}
				key := obj.Pkg().Path() + "." + obj.Name()
				return key, used[key]
			default:
				return "", false
			}
		}
		return "", false
	}
	var findings []finding
	for _, c := range changed {
		if c.Kind != "func" || c.Reason == "removed" || reached[c.Key] {
			continue
		}
		reasons := map[string]bool{}
		for _, fn := range byKey[c.Key] {
			inspectShaping(fn, lookup, reasons)
		}
		if len(reasons) == 0 {
			continue
		}
		list := make([]string, 0, len(reasons))
		for r := range reasons {
			list = append(list, r)
		}
		sort.Strings(list)
		findings = append(findings, finding{Key: c.Key, Kind: "shared_data",
			Reason: "may change data the worker reads: " + strings.Join(list, "; ")})
	}
	return findings
}

func inspectShaping(fn *ssa.Function, lookup typeLookup, reasons map[string]bool) {
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			switch instr := instr.(type) {
			case *ssa.FieldAddr:
				if key, worker := lookup(instr.X.Type()); worker && storedTo(instr) {
					reasons["sets fields of worker-used "+shortKey(key)] = true
				}
			case *ssa.MapUpdate:
				if m, ok := types.Unalias(instr.Map.Type()).Underlying().(*types.Map); ok && freeForm(m) &&
					!responseOnly(instr.Map, 0) {
					reasons["builds a free-form string-keyed map"] = true
				}
			case ssa.CallInstruction:
				callReason(instr.Common(), lookup, reasons)
			}
		}
	}
	for _, anon := range fn.AnonFuncs {
		inspectShaping(anon, lookup, reasons)
	}
}

func storedTo(addr *ssa.FieldAddr) bool {
	for _, ref := range *addr.Referrers() {
		if store, ok := ref.(*ssa.Store); ok && store.Addr == addr {
			return true
		}
	}
	return false
}

func freeForm(m *types.Map) bool {
	key, ok := m.Key().Underlying().(*types.Basic)
	return ok && key.Info()&types.IsString != 0 && types.IsInterface(m.Elem())
}

// responseOnly reports whether a map literal ends up only in a gin response
// (c.JSON(…, gin.H{…})), directly or nested inside another such map. Those
// never reach shared storage.
func responseOnly(v ssa.Value, depth int) bool {
	if depth > 8 {
		return false
	}
	if _, ok := v.(*ssa.MakeMap); !ok {
		return false
	}
	for _, ref := range *v.Referrers() {
		switch ref := ref.(type) {
		case *ssa.MapUpdate:
			if ref.Value == v && !responseOnly(ref.Map, depth+1) {
				return false
			}
		case *ssa.Lookup, *ssa.Range, *ssa.DebugRef:
		case *ssa.MakeInterface:
			for _, use := range *ref.Referrers() {
				switch use := use.(type) {
				case *ssa.MapUpdate:
					if use.Value != ref || !responseOnly(use.Map, depth+1) {
						return false
					}
				case ssa.CallInstruction:
					if !ginCall(use.Common()) {
						return false
					}
				default:
					return false
				}
			}
		case ssa.CallInstruction:
			if !ginCall(ref.Common()) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func ginCall(call *ssa.CallCommon) bool {
	callee := call.StaticCallee()
	if callee == nil {
		return false
	}
	obj, ok := callee.Object().(*types.Func)
	return ok && obj.Pkg() != nil && strings.HasPrefix(obj.Pkg().Path(), "github.com/gin-gonic/gin")
}

func shortKey(key string) string {
	return key[strings.LastIndex(key, "/")+1:]
}

func callReason(call *ssa.CallCommon, lookup typeLookup, reasons map[string]bool) {
	var name, pkg string
	method := false
	if call.IsInvoke() {
		name, method = call.Method.Name(), true
		if call.Method.Pkg() != nil {
			pkg = call.Method.Pkg().Path()
		}
		if named, ok := types.Unalias(call.Value.Type()).(*types.Named); ok && named.Obj().Pkg() != nil {
			pkg = named.Obj().Pkg().Path()
		}
	} else if callee := call.StaticCallee(); callee != nil {
		name = callee.Name()
		if origin := callee.Origin(); origin != nil {
			callee = origin
		}
		if obj, ok := callee.Object().(*types.Func); ok && obj.Pkg() != nil {
			pkg = obj.Pkg().Path()
			method = obj.Type().(*types.Signature).Recv() != nil
		}
	}
	switch {
	case pkg == "":
	case strings.HasPrefix(pkg, "github.com/go-redis/redis") || strings.HasPrefix(pkg, "github.com/redis/go-redis"):
		if redisWrites[name] {
			reasons["writes Redis ("+name+")"] = true
		}
	case pkg == "os" && !method && fileWrites[name]:
		reasons["writes files (os."+name+")"] = true
	case (pkg == "encoding/json" || strings.Contains(pkg, "bytedance/sonic") || strings.Contains(pkg, "goccy/go-json") ||
		strings.HasSuffix(pkg, "new-api/common")) && (name == "Marshal" || name == "MarshalIndent" ||
		name == "MarshalString" || name == "Encode"):
		reasons["encodes JSON"] = true
	case pkg == "gorm.io/gorm" && method && gormWrites[name]:
		target, known := gormTarget(call, name)
		if !known {
			reasons["writes database rows it cannot identify ("+name+")"] = true
		} else if key, worker := lookup(target); worker {
			reasons["writes worker-used rows "+shortKey(key)+" ("+name+")"] = true
		}
	}
}

// gormTarget resolves the model a GORM write touches: the value argument of
// Create/Save/Delete/FirstOrCreate, or Model(x) up the receiver chain.
func gormTarget(call *ssa.CallCommon, name string) (types.Type, bool) {
	if len(call.Args) == 0 {
		return nil, false
	}
	switch name {
	case "Create", "CreateInBatches", "Save", "Delete", "FirstOrCreate":
		if len(call.Args) > 1 {
			if t := concreteArg(call.Args[1]); t != nil {
				return t, true
			}
		}
	case "Exec", "Raw":
		return nil, false
	}
	receiver := call.Args[0]
	for depth := 0; depth < 32; depth++ {
		chained, ok := receiver.(*ssa.Call)
		if !ok {
			return nil, false
		}
		callee := chained.Call.StaticCallee()
		if callee == nil || len(chained.Call.Args) == 0 {
			return nil, false
		}
		switch {
		case callee.Name() == "Model" && len(chained.Call.Args) > 1:
			if t := concreteArg(chained.Call.Args[1]); t != nil {
				return t, true
			}
			return nil, false
		case gormChain[callee.Name()]:
			receiver = chained.Call.Args[0]
		default:
			return nil, false
		}
	}
	return nil, false
}

func concreteArg(v ssa.Value) types.Type {
	if mi, ok := v.(*ssa.MakeInterface); ok {
		return mi.X.Type()
	}
	if types.IsInterface(v.Type()) {
		return nil
	}
	return v.Type()
}
