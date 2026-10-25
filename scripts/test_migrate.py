import importlib.util,pathlib,sqlite3,unittest,json
spec=importlib.util.spec_from_file_location('migration',pathlib.Path(__file__).with_name('migrate-hue-v2.py'));migration=importlib.util.module_from_spec(spec);spec.loader.exec_module(migration)
class MigrationTest(unittest.TestCase):
 def setUp(self):
  self.db=sqlite3.connect(':memory:');self.db.executescript('CREATE TABLE resource_state(kind TEXT,id TEXT,payload TEXT,version INTEGER);CREATE TABLE kv_store(bucket TEXT,key TEXT,value TEXT);')
 def tearDown(self):self.db.close()
 def test_units_identity_and_opaque_kv(self):
  self.db.execute('INSERT INTO resource_state VALUES(?,?,?,1)',('group','1','{"bri":127,"transitiontime":4,"on":false,"scene_name":"Relax"}'))
  self.db.execute('INSERT INTO kv_store VALUES(?,?,?)',('scenes','1','"Relax"'));self.db.execute('INSERT INTO kv_store VALUES(?,?,?)',('private','1','"opaque"'))
  ident='10000000-0000-0000-0000-000000000001';migration.migrate(self.db,{'groups':{'1':ident}},'scenes')
  id,payload=self.db.execute('SELECT id,payload FROM resource_state').fetchone();value=json.loads(payload)
  self.assertEqual(id,ident);self.assertEqual(value['brightness'],50);self.assertEqual(value['duration'],400);self.assertFalse(value['on'])
  self.assertEqual(self.db.execute("SELECT key FROM kv_store WHERE bucket='private'").fetchone()[0],'1')
 def test_unsupported_hs_is_not_guessed(self):
  with self.assertRaises(ValueError):migration.convert('{"hue":0,"sat":254}')
 def test_missing_mapping_is_not_discarded(self):
  self.db.execute('INSERT INTO resource_state VALUES(?,?,?,1)',('light','9','{}'))
  with self.assertRaises(ValueError):migration.migrate(self.db,{})
if __name__=='__main__':unittest.main()
