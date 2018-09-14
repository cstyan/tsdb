package tsdb

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"reflect"
	"testing"

	"github.com/prometheus/tsdb/chunks"

	"github.com/prometheus/tsdb/index"
	"github.com/prometheus/tsdb/labels"
)

func copyFiles(tmpIndex, tmpMeta *os.File, dir string) error {
	// Create copies of files we will modify in tmp.

	indexContents, err := os.Open(fmt.Sprintf("%sindex", dir))
	if err != nil {
		return err
	}
	metaContents, err := os.Open(fmt.Sprintf("%smeta.json", dir))
	if err != nil {
		return err
	}
	_, err = io.Copy(tmpMeta, metaContents)
	if err != nil {
		return err
	}
	_, err = io.Copy(tmpIndex, indexContents)
	if err != nil {
		return err
	}
	tmpIndex.Close()
	tmpMeta.Close()
	return nil
}

func TestRepairBadIndexVersion(t *testing.T) {
	// The broken index used in this test was written by the following script
	// at a broken revision.
	//
	// func main() {
	// 	w, err := index.NewWriter("index")
	// 	if err != nil {
	// 		panic(err)
	// 	}
	// 	err = w.AddSymbols(map[string]struct{}{
	// 		"a": struct{}{},
	// 		"b": struct{}{},
	// 		"1": struct{}{},
	// 		"2": struct{}{},
	// 	})
	// 	if err != nil {
	// 		panic(err)
	// 	}
	// 	err = w.AddSeries(1, labels.FromStrings("a", "1", "b", "1"))
	// 	if err != nil {
	// 		panic(err)
	// 	}
	// 	err = w.AddSeries(2, labels.FromStrings("a", "2", "b", "1"))
	// 	if err != nil {
	// 		panic(err)
	// 	}
	// 	err = w.WritePostings("b", "1", index.NewListPostings([]uint64{1, 2}))
	// 	if err != nil {
	// 		panic(err)
	// 	}
	// 	if err := w.Close(); err != nil {
	// 		panic(err)
	// 	}
	// }

	const baseDir = "testdata/repair_index_version"
	const tempDir = "tempDir"
	dbDir := fmt.Sprintf("%s/01BZJ9WJQPWHGNC2W4J9TA62KC/", baseDir)

	err := os.Mkdir(tempDir, 0755)
	if err != nil {
		t.Fatal(err)
	}
	tmpIndex, err := ioutil.TempFile(tempDir, "index")
	if err != nil {
		t.Fatal(err)
	}
	tmpMeta, err := ioutil.TempFile(tempDir, "meta.json")
	if err != nil {
		t.Fatal(err)
	}

	err = copyFiles(tmpIndex, tmpMeta, dbDir)
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		os.Rename(tmpIndex.Name(), fmt.Sprintf("%sindex", dbDir))
		os.Rename(tmpMeta.Name(), fmt.Sprintf("%smeta.json", dbDir))
		os.RemoveAll(tempDir)
		os.RemoveAll(fmt.Sprintf("%s/wal/", baseDir))
		os.RemoveAll(fmt.Sprintf("%s/lock", baseDir))
	}()

	// In its current state, lookups should fail with the fixed code.
	meta, err := readMetaFile(dbDir)
	if err == nil {
		t.Fatal("error expected but got none")
	}
	// Touch chunks dir in block.
	os.MkdirAll(dbDir+"chunks", 0777)

	r, err := index.NewFileReader(dbDir + "index")
	if err != nil {
		t.Fatal(err)
	}
	p, err := r.Postings("b", "1")
	if err != nil {
		t.Fatal(err)
	}
	for p.Next() {
		t.Logf("next ID %d", p.At())

		var lset labels.Labels
		if err := r.Series(p.At(), &lset, nil); err == nil {
			t.Fatal("expected error but got none")
		}
	}
	if p.Err() != nil {
		t.Fatal(err)
	}

	// On DB opening all blocks in the base dir should be repaired.
	db, err := Open("testdata/repair_index_version", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	r, err = index.NewFileReader(dbDir + "index")
	if err != nil {
		t.Fatal(err)
	}
	p, err = r.Postings("b", "1")
	if err != nil {
		t.Fatal(err)
	}
	res := []labels.Labels{}

	for p.Next() {
		t.Logf("next ID %d", p.At())

		var lset labels.Labels
		var chks []chunks.Meta
		if err := r.Series(p.At(), &lset, &chks); err != nil {
			t.Fatal(err)
		}
		res = append(res, lset)
	}
	if p.Err() != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res, []labels.Labels{
		{{"a", "1"}, {"b", "1"}},
		{{"a", "2"}, {"b", "1"}},
	}) {
		t.Fatalf("unexpected result %v", res)
	}

	meta, err = readMetaFile(dbDir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Version != 1 {
		t.Fatalf("unexpected meta version %d", meta.Version)
	}
}
