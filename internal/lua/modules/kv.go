package modules

import (
	"time"

	"github.com/rs/zerolog/log"
	lua "github.com/yuin/gopher-lua"

	"github.com/dokzlo13/lightd/internal/kv"
)

const bucketTypeName = "kv_bucket"

// KVModule provides the kv module to Lua.
type KVModule struct {
	manager *kv.Manager
}

// NewKVModule creates a new KV module.
func NewKVModule(manager *kv.Manager) *KVModule {
	return &KVModule{manager: manager}
}

// Loader is the module loader for Lua.
func (m *KVModule) Loader(L *lua.LState) int {
	// Register bucket userdata type
	mt := L.NewTypeMetatable(bucketTypeName)
	L.SetField(mt, "__index", L.SetFuncs(L.NewTable(), bucketMethods))

	// Create module table
	mod := L.NewTable()

	L.SetField(mod, "bucket", L.NewFunction(m.bucket))
	L.SetField(mod, "exists", L.NewFunction(m.exists))
	L.SetField(mod, "delete", L.NewFunction(m.delete))
	L.SetField(mod, "list", L.NewFunction(m.list))

	L.Push(mod)
	return 1
}

// bucket(name, opts) -> Bucket
// opts: { persistent = true/false }
func (m *KVModule) bucket(L *lua.LState) int {
	L.CheckTable(1) // self
	name := L.CheckString(2)

	// Parse options
	persistent := true // default to persistent
	if opts := L.OptTable(3, nil); opts != nil {
		if p := L.GetField(opts, "persistent"); p != lua.LNil {
			persistent = lua.LVAsBool(p)
		}
	}

	bucket := m.manager.Bucket(name, persistent)

	// Create userdata with bucket
	ud := L.NewUserData()
	ud.Value = bucket
	L.SetMetatable(ud, L.GetTypeMetatable(bucketTypeName))

	L.Push(ud)
	return 1
}

// exists(name) -> bool
func (m *KVModule) exists(L *lua.LState) int {
	L.CheckTable(1) // self
	name := L.CheckString(2)

	L.Push(lua.LBool(m.manager.Exists(name)))
	return 1
}

// delete(name) -> bool
func (m *KVModule) delete(L *lua.LState) int {
	L.CheckTable(1) // self
	name := L.CheckString(2)

	deleted, err := m.manager.Delete(name)
	if err != nil {
		log.Warn().Err(err).Str("bucket", name).Msg("Failed to delete bucket")
		L.Push(lua.LFalse)
		return 1
	}

	L.Push(lua.LBool(deleted))
	return 1
}

// list() -> table
func (m *KVModule) list(L *lua.LState) int {
	L.CheckTable(1) // self

	names, err := m.manager.List()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to list buckets")
		L.Push(L.NewTable())
		return 1
	}

	tbl := L.NewTable()
	for i, name := range names {
		tbl.RawSetInt(i+1, lua.LString(name))
	}

	L.Push(tbl)
	return 1
}

// Bucket methods accessible from Lua
var bucketMethods = map[string]lua.LGFunction{
	"store":  bucketStore,
	"get":    bucketGet,
	"exists": bucketExists,
	"delete": bucketDelete,
	"keys":   bucketKeys,
	"clear":  bucketClear,
}

// checkBucket extracts the bucket from userdata at the given stack position.
func checkBucket(L *lua.LState, pos int) kv.Bucket {
	ud := L.CheckUserData(pos)
	if bucket, ok := ud.Value.(kv.Bucket); ok {
		return bucket
	}
	L.ArgError(pos, "bucket expected")
	return nil
}

// store(key, value, opts) -> nil
// opts: { ttl = seconds }
func bucketStore(L *lua.LState) int {
	bucket := checkBucket(L, 1)
	key := L.CheckString(2)
	value := LuaToGo(L.Get(3))

	// Parse options
	var opts *kv.StoreOptions
	if optsTable := L.OptTable(4, nil); optsTable != nil {
		opts = &kv.StoreOptions{}
		if ttl := L.GetField(optsTable, "ttl"); ttl != lua.LNil {
			if ttlNum, ok := ttl.(lua.LNumber); ok {
				opts.TTL = time.Duration(ttlNum) * time.Second
			}
		}
	}

	if err := bucket.Store(key, value, opts); err != nil {
		log.Warn().Err(err).
			Str("bucket", bucket.Name()).
			Str("key", key).
			Msg("Failed to store value")
	}

	return 0
}

// get(key) -> value | nil
func bucketGet(L *lua.LState) int {
	bucket := checkBucket(L, 1)
	key := L.CheckString(2)

	value, err := bucket.Get(key)
	if err != nil {
		log.Warn().Err(err).
			Str("bucket", bucket.Name()).
			Str("key", key).
			Msg("Failed to get value")
		L.Push(lua.LNil)
		return 1
	}

	if value == nil {
		L.Push(lua.LNil)
		return 1
	}

	L.Push(GoToLuaValue(L, value))
	return 1
}

// exists(key) -> bool
func bucketExists(L *lua.LState) int {
	bucket := checkBucket(L, 1)
	key := L.CheckString(2)

	exists, err := bucket.Exists(key)
	if err != nil {
		log.Warn().Err(err).
			Str("bucket", bucket.Name()).
			Str("key", key).
			Msg("Failed to check key existence")
		L.Push(lua.LFalse)
		return 1
	}

	L.Push(lua.LBool(exists))
	return 1
}

// delete(key) -> bool
func bucketDelete(L *lua.LState) int {
	bucket := checkBucket(L, 1)
	key := L.CheckString(2)

	deleted, err := bucket.Delete(key)
	if err != nil {
		log.Warn().Err(err).
			Str("bucket", bucket.Name()).
			Str("key", key).
			Msg("Failed to delete key")
		L.Push(lua.LFalse)
		return 1
	}

	L.Push(lua.LBool(deleted))
	return 1
}

// keys() -> table
func bucketKeys(L *lua.LState) int {
	bucket := checkBucket(L, 1)

	keys, err := bucket.Keys()
	if err != nil {
		log.Warn().Err(err).
			Str("bucket", bucket.Name()).
			Msg("Failed to list keys")
		L.Push(L.NewTable())
		return 1
	}

	tbl := L.NewTable()
	for i, key := range keys {
		tbl.RawSetInt(i+1, lua.LString(key))
	}

	L.Push(tbl)
	return 1
}

// clear() -> nil
func bucketClear(L *lua.LState) int {
	bucket := checkBucket(L, 1)

	if err := bucket.Clear(); err != nil {
		log.Warn().Err(err).
			Str("bucket", bucket.Name()).
			Msg("Failed to clear bucket")
	}

	return 0
}

