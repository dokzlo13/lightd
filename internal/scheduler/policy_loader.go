package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/ast"
	"github.com/yuin/gopher-lua/parse"
)

const maxPolicyBytes = 1 << 20

func readLuaFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxPolicyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxPolicyBytes {
		return nil, fmt.Errorf("Lua source exceeds 1 MiB")
	}
	return data, nil
}

// CheckLuaSyntax compiles ordinary automation Lua without executing it.
func CheckLuaSyntax(path string) error {
	data, err := readLuaFile(path)
	if err != nil {
		return err
	}
	chunk, err := parse.Parse(bytes.NewReader(data), path)
	if err != nil {
		return err
	}
	_, err = lua.Compile(chunk, path)
	return err
}

// LoadPolicyFile executes a pure declaration in a separate VM with no libraries,
// modules, filesystem, network, storage, or action access. A finite CPU budget and
// bounded table conversion prevent unbounded policy evaluation.
func LoadPolicyFile(ctx context.Context, path string) (PolicySpec, error) {
	data, err := readLuaFile(path)
	if err != nil {
		return PolicySpec{}, err
	}
	chunk, err := parse.Parse(bytes.NewReader(data), path)
	if err != nil {
		return PolicySpec{}, err
	}
	if len(chunk) != 1 {
		return PolicySpec{}, fmt.Errorf("pure policy must contain only return { ... }")
	}
	ret, ok := chunk[0].(*ast.ReturnStmt)
	if !ok || len(ret.Exprs) != 1 {
		return PolicySpec{}, fmt.Errorf("pure policy must return one literal table")
	}
	if _, ok := ret.Exprs[0].(*ast.TableExpr); !ok {
		return PolicySpec{}, fmt.Errorf("pure policy must return a literal table")
	}
	budget := 10000
	if err := validateLiteral(ret.Exprs[0], 0, &budget); err != nil {
		return PolicySpec{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	L := lua.NewState(lua.Options{SkipOpenLibs: true, CallStackSize: 128, RegistrySize: 1024, RegistryMaxSize: 16384})
	defer L.Close()
	L.SetContext(ctx)
	fn, err := L.Load(bytes.NewReader(data), path)
	if err != nil {
		return PolicySpec{}, err
	}
	L.Push(fn)
	if err = L.PCall(0, 1, nil); err != nil {
		return PolicySpec{}, fmt.Errorf("pure policy: %w", err)
	}
	table, ok := L.Get(-1).(*lua.LTable)
	if !ok {
		return PolicySpec{}, fmt.Errorf("policy file must return one table")
	}
	return PolicySpecFromLua(table)
}

// PolicySpecFromLua validates and copies a bounded declarative table. It is also
// used by sched.policy, so offline and live parsing cannot diverge.
func PolicySpecFromLua(table *lua.LTable) (PolicySpec, error) {
	budget := 10000
	seen := map[*lua.LTable]bool{}
	var convert func(lua.LValue, int) (any, error)
	convert = func(value lua.LValue, depth int) (any, error) {
		budget--
		if budget < 0 || depth > 24 {
			return nil, fmt.Errorf("policy table exceeds size/depth limits")
		}
		switch v := value.(type) {
		case lua.LString:
			return string(v), nil
		case lua.LNumber:
			return float64(v), nil
		case lua.LBool:
			return bool(v), nil
		case *lua.LNilType:
			return nil, nil
		case *lua.LTable:
			if seen[v] {
				return nil, fmt.Errorf("policy table contains cycle or shared table")
			}
			seen[v] = true
			defer delete(seen, v)
			n := v.Len()
			if n > budget {
				return nil, fmt.Errorf("policy array exceeds size limit")
			}
			if n > 0 {
				out := make([]any, n)
				var err error
				count := 0
				v.ForEach(func(k, val lua.LValue) {
					count++
					num, ok := k.(lua.LNumber)
					if !ok || int(num) < 1 || int(num) > n || float64(int(num)) != float64(num) {
						err = fmt.Errorf("policy arrays must have contiguous integer keys")
					}
				})
				if err != nil || count != n {
					return nil, fmt.Errorf("policy arrays must have contiguous integer keys")
				}
				for i := range out {
					out[i], err = convert(v.RawGetInt(i+1), depth+1)
					if err != nil {
						return nil, err
					}
				}
				return out, nil
			}
			out := map[string]any{}
			var err error
			v.ForEach(func(k, val lua.LValue) {
				if err != nil {
					return
				}
				key, ok := k.(lua.LString)
				if !ok {
					err = fmt.Errorf("policy object keys must be strings")
					return
				}
				out[string(key)], err = convert(val, depth+1)
			})
			return out, err
		default:
			return nil, fmt.Errorf("policy values must be data, got %s", value.Type())
		}
	}
	value, err := convert(table, 0)
	if err != nil {
		return PolicySpec{}, err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return PolicySpec{}, err
	}
	var spec PolicySpec
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&spec); err != nil {
		return spec, fmt.Errorf("policy definition: %w", err)
	}
	return spec, nil
}

// Restrict source as well as VM capabilities: loops, calls, arithmetic that can
// allocate large values, and dynamic definitions are rejected before execution.
func validateLiteral(expr ast.Expr, depth int, budget *int) error {
	*budget--
	if *budget < 0 || depth > 24 {
		return fmt.Errorf("policy literal exceeds size/depth limits")
	}
	switch v := expr.(type) {
	case *ast.StringExpr, *ast.NumberExpr, *ast.TrueExpr, *ast.FalseExpr, *ast.NilExpr:
		return nil
	case *ast.UnaryMinusOpExpr:
		if _, ok := v.Expr.(*ast.NumberExpr); !ok {
			return fmt.Errorf("policy permits unary minus only on numeric literals")
		}
		return nil
	case *ast.TableExpr:
		for _, field := range v.Fields {
			if field.Key != nil {
				if err := validateLiteral(field.Key, depth+1, budget); err != nil {
					return err
				}
			}
			if err := validateLiteral(field.Value, depth+1, budget); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("policy must contain literal data only (no calls, expressions, or functions)")
	}
}
