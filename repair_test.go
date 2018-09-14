package tsdb

import (
	"io"
	"os"
	"reflect"
	"testing"

	"github.com/prometheus/tsdb/chunks"

	"github.com/prometheus/tsdb/index"
	"github.com/prometheus/tsdb/labels"
)

func copyFiles(copyIndex, copyMeta *os.File, dir string) error {
	// Create copies of files we will modify in tmp.

	indexContents, err := os.Open(dir + "index")
	if err != nil {
		return err
	}
	metaContents, err := os.Open(dir + "meta.json")
	if err != nil {
		return err
	}
	_, err = io.Copy(copyMeta, metaContents)
	if err != nil {
		return err
	}
	_, err = io.Copy(copyIndex, indexContents)
	if err != nil {
		return err
	}
	copyIndex.Close()
	copyMeta.Close()
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

	const testDir = "testdata/repair_index_version"
	const testDirBlock = testDir + "/01BZJ9WJQPWHGNC2W4J9TA62KC/"

	const copyDir = "copyDir/"
	const copyDirBlock = copyDir + "01BZJ9WJQPWHGNC2W4J9TA62KC/"

	err := os.MkdirAll(copyDirBlock, 0755)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(copyDir)

	copyIndex, err := os.Create(copyDirBlock + "index")
	if err != nil {
		t.Fatal(err)
	}
	copyMeta, err := os.Create(copyDirBlock + "meta.json")
	if err != nil {
		t.Fatal(err)
	}

	err = copyFiles(copyIndex, copyMeta, testDirBlock)
	if err != nil {
		t.Fatal(err)
	}

	// In its current state, lookups should fail with the fixed code.
	meta, err := readMetaFile(copyDirBlock)
	if err == nil {
		t.Fatal("error expected but got none")
	}
	// Touch chunks dir in block.
	os.MkdirAll(copyDirBlock+"chunks", 0777)

	r, err := index.NewFileReader(copyDirBlock + "index")
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
	db, err := Open(copyDir, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	r, err = index.NewFileReader(copyDirBlock + "index")
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

	meta, err = readMetaFile(copyDirBlock)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Version != 1 {
		t.Fatalf("unexpected meta version %d", meta.Version)
	}
}
