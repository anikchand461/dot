package document

import (
	"bytes"
	"os"
	"testing"
	"time"
)

// BIG=/path/big.xopp go test ./internal/document -run BigTimings -v
func TestBigTimings(t *testing.T) {
	p := os.Getenv("BIG")
	if p == "" {
		t.Skip()
	}
	t0 := time.Now()
	d, err := OpenFile(p)
	if err != nil {
		t.Fatal(err)
	}
	t1 := time.Now()
	var b bytes.Buffer
	Save(&b, d)
	t2 := time.Now()
	var raw bytes.Buffer
	Encode(&raw, d)
	t3 := time.Now()
	t.Logf("load %v | save(gzip) %v | encode only %v | %d MB xml", t1.Sub(t0).Round(time.Millisecond), t2.Sub(t1).Round(time.Millisecond), t3.Sub(t2).Round(time.Millisecond), raw.Len()>>20)
}
