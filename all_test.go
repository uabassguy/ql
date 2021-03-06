// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ql

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"path"
	"runtime"
	"strings"
	"testing"
)

func init() {
	log.SetFlags(log.Flags() | log.Lshortfile)
}

func dbg(s string, va ...interface{}) {
	_, fn, fl, _ := runtime.Caller(1)
	fmt.Printf("dbg %s:%d: ", path.Base(fn), fl)
	fmt.Printf(s, va...)
	fmt.Println()
}

func caller(s string, va ...interface{}) {
	_, fn, fl, _ := runtime.Caller(2)
	fmt.Printf("caller: %s:%d: ", path.Base(fn), fl)
	fmt.Printf(s, va...)
	fmt.Println()
	_, fn, fl, _ = runtime.Caller(1)
	fmt.Printf("\tcallee: %s:%d: ", path.Base(fn), fl)
	fmt.Println()
}

func use(...interface{}) {}

func dumpTables3(r *root) {
	fmt.Printf("---- r.head %d, r.thead %p\n", r.head, r.thead)
	for k, v := range r.tables {
		fmt.Printf("%p: %s->%+v\n", v, k, v)
	}
	fmt.Println("<exit>")
}

func dumpTables2(s storage) {
	fmt.Println("****")
	h := int64(1)
	for h != 0 {
		d, err := s.Read(nil, h)
		if err != nil {
			log.Fatal(err)
		}

		fmt.Printf("%d: %v\n", h, d)
		h = d[0].(int64)
	}
	fmt.Println("<exit>")
}

func (db *DB) dumpTables() string {
	var buf bytes.Buffer
	for k, v := range db.root.tables {
		buf.WriteString(fmt.Sprintf("%s->%v, %v\n", k, v.h, v.next))
	}
	return buf.String()
}

func stStr(st int) string {
	switch st {
	case stDisabled:
		return "stDisabled"
	case stIdle:
		return "stIdle"
	case stCollecting:
		return "stCollecting"
	case stIdleArmed:
		return "stIdleArmed"
	case stCollectingArmed:
		return "stCollectingArmed"
	case stCollectingTriggered:
		return "stCollectingTriggered"
	}
	return fmt.Sprintf("state(%d)", st)
}

type testDB interface {
	setup() (db *DB, err error)
	mark() (err error)
	teardown() (err error)
}

var (
	_ testDB = (*fileTestDB)(nil)
	_ testDB = (*memTestDB)(nil)
)

type memTestDB struct {
	db *DB
	m0 int64
}

func (m *memTestDB) setup() (db *DB, err error) {
	if m.db, err = OpenMem(); err != nil {
		return
	}

	return m.db, nil
}

func (m *memTestDB) mark() (err error) {
	m.m0, err = m.db.store.Verify()
	if err != nil {
		m.m0 = -1
	}
	return
}

func (m *memTestDB) teardown() (err error) {
	if m.m0 < 0 {
		return
	}

	n, err := m.db.store.Verify()
	if err != nil {
		return
	}

	if g, e := n, m.m0; g != e {
		return fmt.Errorf("allocs: got %d, exp %d", g, e)
	}

	return
}

type fileTestDB struct {
	db   *DB
	gmp0 int
	m0   int64
}

func (m *fileTestDB) setup() (db *DB, err error) {
	m.gmp0 = runtime.GOMAXPROCS(0)
	f, err := ioutil.TempFile("", "ql-test-")
	if err != nil {
		return
	}

	if m.db, err = OpenFile(f.Name(), &Options{}); err != nil {
		return
	}

	return m.db, nil
}

func (m *fileTestDB) mark() (err error) {
	m.m0, err = m.db.store.Verify()
	if err != nil {
		m.m0 = -1
	}
	return
}

func (m *fileTestDB) teardown() (err error) {
	runtime.GOMAXPROCS(m.gmp0)
	defer func() {
		f := m.db.store.(*file)
		errSet(&err, m.db.Close())
		os.Remove(f.f0.Name())
		if f.wal != nil {
			os.Remove(f.wal.Name())
		}
	}()

	if m.m0 < 0 {
		return
	}

	n, err := m.db.store.Verify()
	if err != nil {
		return
	}

	if g, e := n, m.m0; g != e {
		return fmt.Errorf("allocs: got %d, exp %d", g, e)
	}
	return
}

type osFileTestDB struct {
	db   *DB
	gmp0 int
	m0   int64
}

func (m *osFileTestDB) setup() (db *DB, err error) {
	m.gmp0 = runtime.GOMAXPROCS(0)
	f, err := ioutil.TempFile("", "ql-test-osfile")
	if err != nil {
		return
	}

	if m.db, err = OpenFile("", &Options{OSFile: f}); err != nil {
		return
	}

	return m.db, nil
}

func (m *osFileTestDB) mark() (err error) {
	m.m0, err = m.db.store.Verify()
	if err != nil {
		m.m0 = -1
	}
	return
}

func (m *osFileTestDB) teardown() (err error) {
	runtime.GOMAXPROCS(m.gmp0)
	defer func() {
		f := m.db.store.(*file)
		errSet(&err, m.db.Close())
		os.Remove(f.f0.Name())
		if f.wal != nil {
			os.Remove(f.wal.Name())
		}
	}()

	if m.m0 < 0 {
		return
	}

	n, err := m.db.store.Verify()
	if err != nil {
		return
	}

	if g, e := n, m.m0; g != e {
		return fmt.Errorf("allocs: got %d, exp %d", g, e)
	}
	return
}

func TestMemStorage(t *testing.T) {
	test(t, &memTestDB{})
}

func TestFileStorage(t *testing.T) {
	test(t, &fileTestDB{})
}

func TestOSFileStorage(t *testing.T) {
	test(t, &osFileTestDB{})
}

var (
	compiledCommit        = MustCompile("COMMIT;")
	compiledCreate        = MustCompile("BEGIN TRANSACTION; CREATE TABLE t (i16 int16, s16 string, s string);")
	compiledCreate2       = MustCompile("BEGIN TRANSACTION; CREATE TABLE t (i16 int16, s16 string, s string); COMMIT;")
	compiledIns           = MustCompile("INSERT INTO t VALUES($1, $2, $3);")
	compiledSelect        = MustCompile("SELECT * FROM t;")
	compiledSelectOrderBy = MustCompile("SELECT * FROM t ORDER BY i16, s16;")
	compiledTrunc         = MustCompile("BEGIN TRANSACTION; TRUNCATE TABLE t; COMMIT;")
)

func rnds16(rng *rand.Rand, n int) string {
	a := make([]string, n)
	for i := range a {
		a[i] = fmt.Sprintf("%016x", rng.Int63())
	}
	return strings.Join(a, "")
}

func benchmarkSelect(b *testing.B, n int, sel List, ts testDB) {
	db, err := ts.setup()
	if err != nil {
		b.Error(err)
		return
	}

	defer ts.teardown()

	ctx := NewRWCtx()
	if _, i, err := db.Execute(ctx, compiledCreate); err != nil {
		b.Error(i, err)
		return
	}

	rng := rand.New(rand.NewSource(42))
	for i := 0; i < n; i++ {
		if _, _, err := db.Execute(ctx, compiledIns, int16(rng.Int()), rnds16(rng, 1), rnds16(rng, 63)); err != nil {
			b.Error(err)
			return
		}
	}

	if _, i, err := db.Execute(ctx, compiledCommit); err != nil {
		b.Error(i, err)
		return
	}

	b.SetBytes(int64(n) * (2 + 1024))
	runtime.GC()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rs, index, err := db.Execute(nil, sel)
		if err != nil {
			b.Error(index, err)
			return
		}

		if err = rs[0].Do(false, func(record []interface{}) (bool, error) { return true, nil }); err != nil {
			b.Errorf("%v %T(%#v)", err, err, err)
			return
		}
	}
	b.StopTimer()

}

func BenchmarkSelectMem1kBx1e2(b *testing.B) {
	benchmarkSelect(b, 1e2, compiledSelect, &memTestDB{})
}

func BenchmarkSelectFile1kBx1e2(b *testing.B) {
	benchmarkSelect(b, 1e2, compiledSelect, &fileTestDB{})
}

func BenchmarkSelectMem1kBx1e3(b *testing.B) {
	benchmarkSelect(b, 1e3, compiledSelect, &memTestDB{})
}

func BenchmarkSelectFile1kBx1e3(b *testing.B) {
	benchmarkSelect(b, 1e3, compiledSelect, &fileTestDB{})
}

func BenchmarkSelectMem1kBx1e4(b *testing.B) {
	benchmarkSelect(b, 1e4, compiledSelect, &memTestDB{})
}

func BenchmarkSelectFile1kBx1e4(b *testing.B) {
	benchmarkSelect(b, 1e4, compiledSelect, &fileTestDB{})
}

func BenchmarkSelectMem1kBx1e5(b *testing.B) {
	benchmarkSelect(b, 1e5, compiledSelect, &memTestDB{})
}

func BenchmarkSelectFile1kBx1e5(b *testing.B) {
	benchmarkSelect(b, 1e5, compiledSelect, &fileTestDB{})
}

func BenchmarkSelectOrderedMem1kBx1e2(b *testing.B) {
	benchmarkSelect(b, 1e2, compiledSelectOrderBy, &memTestDB{})
}

func BenchmarkSelectOrderedFile1kBx1e2(b *testing.B) {
	benchmarkSelect(b, 1e2, compiledSelectOrderBy, &fileTestDB{})
}

func BenchmarkSelectOrderedMem1kBx1e3(b *testing.B) {
	benchmarkSelect(b, 1e3, compiledSelectOrderBy, &memTestDB{})
}

func BenchmarkSelectOrderedFile1kBx1e3(b *testing.B) {
	benchmarkSelect(b, 1e3, compiledSelectOrderBy, &fileTestDB{})
}

func BenchmarkSelectOrderedMem1kBx1e4(b *testing.B) {
	benchmarkSelect(b, 1e4, compiledSelectOrderBy, &memTestDB{})
}

func BenchmarkSelectOrderedFile1kBx1e4(b *testing.B) {
	benchmarkSelect(b, 1e4, compiledSelectOrderBy, &fileTestDB{})
}

func TestString(t *testing.T) {
	for _, v := range testdata {
		a := strings.Split(v, "\n|")
		v = a[0]
		v = strings.Replace(v, "&or;", "|", -1)
		v = strings.Replace(v, "&oror;", "||", -1)
		l, err := Compile(v)
		if err != nil {
			continue
		}

		if s := l.String(); len(s) == 0 {
			t.Fatal("List.String() returned empty string")
		}
	}
}

func benchmarkInsert(b *testing.B, batch, total int, ts testDB) {
	if total%batch != 0 {
		b.Fatal("internal error")
	}

	db, err := ts.setup()
	if err != nil {
		b.Error(err)
		return
	}

	defer ts.teardown()

	ctx := NewRWCtx()
	if _, i, err := db.Execute(ctx, compiledCreate2); err != nil {
		b.Error(i, err)
		return
	}

	s := fmt.Sprintf(`(0, "0123456789abcdef", "%s"),`, rnds16(rand.New(rand.NewSource(42)), 63))
	var buf bytes.Buffer
	buf.WriteString("BEGIN TRANSACTION; INSERT INTO t VALUES\n")
	for i := 0; i < batch; i++ {
		buf.WriteString(s)
	}
	buf.WriteString("; COMMIT;")
	ins, err := Compile(buf.String())
	if err != nil {
		b.Error(err)
		return
	}

	b.SetBytes(int64(total) * (2 + 1024))
	runtime.GC()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for n := 0; n != total; n += batch {
			if _, _, err = db.Execute(ctx, ins); err != nil {
				b.Error(err)
				return
			}
		}
		b.StopTimer()
		if _, _, err = db.Execute(ctx, compiledTrunc); err != nil {
			b.Error(err)
		}
		b.StartTimer()
	}
	b.StopTimer()
}

func BenchmarkInsertMem1kBn1e0t1e2(b *testing.B) {
	benchmarkInsert(b, 1e0, 1e2, &memTestDB{})
}

func BenchmarkInsertFile1kBn1e0t1e2(b *testing.B) {
	benchmarkInsert(b, 1e0, 1e2, &fileTestDB{})
}

func BenchmarkInsertMem1kBn1e0t1e3(b *testing.B) {
	benchmarkInsert(b, 1e0, 1e3, &memTestDB{})
}

func BenchmarkInsertFile1kBn1e0t1e3(b *testing.B) {
	benchmarkInsert(b, 1e0, 1e3, &fileTestDB{})
}

func BenchmarkInsertMem1kBn1e0t1e4(b *testing.B) {
	benchmarkInsert(b, 1e0, 1e4, &memTestDB{})
}

func BenchmarkInsertFile1kBn1e0t1e4(b *testing.B) {
	benchmarkInsert(b, 1e0, 1e4, &fileTestDB{})
}

func BenchmarkInsertMem1kBn1e0t1e5(b *testing.B) {
	benchmarkInsert(b, 1e0, 1e5, &memTestDB{})
}

func BenchmarkInsertFile1kBn1e0t1e5(b *testing.B) {
	benchmarkInsert(b, 1e0, 1e5, &fileTestDB{})
}

func BenchmarkInsertMem1kBn1e1t1e2(b *testing.B) {
	benchmarkInsert(b, 1e1, 1e2, &memTestDB{})
}

func BenchmarkInsertFile1kBn1e1t1e2(b *testing.B) {
	benchmarkInsert(b, 1e1, 1e2, &fileTestDB{})
}

func BenchmarkInsertMem1kBn1e1t1e3(b *testing.B) {
	benchmarkInsert(b, 1e1, 1e3, &memTestDB{})
}

func BenchmarkInsertFile1kBn1e1t1e3(b *testing.B) {
	benchmarkInsert(b, 1e1, 1e3, &fileTestDB{})
}

func BenchmarkInsertMem1kBn1e1t1e4(b *testing.B) {
	benchmarkInsert(b, 1e1, 1e4, &memTestDB{})
}

func BenchmarkInsertFile1kBn1e1t1e4(b *testing.B) {
	benchmarkInsert(b, 1e1, 1e4, &fileTestDB{})
}

func BenchmarkInsertMem1kBn1e1t1e5(b *testing.B) {
	benchmarkInsert(b, 1e1, 1e5, &memTestDB{})
}

func BenchmarkInsertFile1kBn1e1t1e5(b *testing.B) {
	benchmarkInsert(b, 1e1, 1e5, &fileTestDB{})
}

func BenchmarkInsertMem1kBn1e2t1e2(b *testing.B) {
	benchmarkInsert(b, 1e2, 1e2, &memTestDB{})
}

func BenchmarkInsertFile1kBn1e2t1e2(b *testing.B) {
	benchmarkInsert(b, 1e2, 1e2, &fileTestDB{})
}

func BenchmarkInsertMem1kBn1e2t1e3(b *testing.B) {
	benchmarkInsert(b, 1e2, 1e3, &memTestDB{})
}

func BenchmarkInsertFile1kBn1e2t1e3(b *testing.B) {
	benchmarkInsert(b, 1e2, 1e3, &fileTestDB{})
}

func BenchmarkInsertMem1kBn1e2t1e4(b *testing.B) {
	benchmarkInsert(b, 1e2, 1e4, &memTestDB{})
}

func BenchmarkInsertFile1kBn1e2t1e4(b *testing.B) {
	benchmarkInsert(b, 1e2, 1e4, &fileTestDB{})
}

func BenchmarkInsertMem1kBn1e2t1e5(b *testing.B) {
	benchmarkInsert(b, 1e2, 1e5, &memTestDB{})
}

func BenchmarkInsertFile1kBn1e2t1e5(b *testing.B) {
	benchmarkInsert(b, 1e2, 1e5, &fileTestDB{})
}

func BenchmarkInsertMem1kBn1e3t1e3(b *testing.B) {
	benchmarkInsert(b, 1e3, 1e3, &memTestDB{})
}

func BenchmarkInsertFile1kBn1e3t1e3(b *testing.B) {
	benchmarkInsert(b, 1e3, 1e3, &fileTestDB{})
}

func BenchmarkInsertMem1kBn1e3t1e4(b *testing.B) {
	benchmarkInsert(b, 1e3, 1e4, &memTestDB{})
}

func BenchmarkInsertFile1kBn1e3t1e4(b *testing.B) {
	benchmarkInsert(b, 1e3, 1e4, &fileTestDB{})
}

func BenchmarkInsertMem1kBn1e3t1e5(b *testing.B) {
	benchmarkInsert(b, 1e3, 1e5, &memTestDB{})
}

func BenchmarkInsertFile1kBn1e3t1e5(b *testing.B) {
	benchmarkInsert(b, 1e3, 1e5, &fileTestDB{})
}

func TestReopen(t *testing.T) {
	f, err := ioutil.TempFile("", "ql-test-")
	if err != nil {
		t.Fatal(err)
	}

	nm := f.Name()
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}

	defer func() {
		if nm != "" {
			os.Remove(nm)
		}
	}()

	db, err := OpenFile(nm, &Options{})
	if err != nil {
		t.Error(err)
		return
	}

	for _, tn := range "abc" {
		ql := fmt.Sprintf(`
BEGIN TRANSACTION;
	CREATE TABLE %c (i int, s string);
	INSERT INTO %c VALUES (%d, "<%c>");
COMMIT;
		`, tn, tn, tn-'a'+42, tn)
		_, i, err := db.Run(NewRWCtx(), ql)
		if err != nil {
			db.Close()
			t.Error(i, err)
			return
		}
	}

	if err = db.Close(); err != nil {
		t.Error(err)
		return
	}

	db, err = OpenFile(nm, &Options{})
	if err != nil {
		t.Error(err)
		return
	}

	defer func() {
		if err = db.Close(); err != nil {
			t.Error(err)
		}
	}()

	if _, _, err = db.Run(NewRWCtx(), "BEGIN TRANSACTION; DROP TABLE b; COMMIT;"); err != nil {
		t.Error(err)
		return
	}

	for _, tn := range "ac" {
		ql := fmt.Sprintf("SELECT * FROM %c;", tn)
		rs, i, err := db.Run(NewRWCtx(), ql)
		if err != nil {
			t.Error(i, err)
			return
		}

		if rs == nil {
			t.Error(rs)
			return
		}

		rid := 0
		if err = rs[0].Do(false, func(r []interface{}) (bool, error) {
			rid++
			if rid != 1 {
				return false, fmt.Errorf("rid %d", rid)
			}

			if g, e := recStr(r), fmt.Sprintf(`%d, "<%c>"`, tn-'a'+42, tn); g != e {
				return false, fmt.Errorf("g `%s`, e `%s`", g, e)
			}

			return true, nil
		}); err != nil {
			t.Error(err)
			return
		}
	}
}

func recStr(data []interface{}) string {
	a := make([]string, len(data))
	for i, v := range data {
		switch x := v.(type) {
		case string:
			a[i] = fmt.Sprintf("%q", x)
		default:
			a[i] = fmt.Sprint(x)
		}
	}
	return strings.Join(a, ", ")
}

//TODO +test long blob types with multiple inner chunks.
