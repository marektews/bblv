package hmsfile

import (
	"bytes"
	"reflect"
	"testing"
)

func TestEncodeDecode(t *testing.T) {
	want := File{Version: "1", HMS: Table{"0300010000010007": {"": "Heatbed fault", "039": "A1 heatbed fault"}}, Errors: Table{}}
	var buf bytes.Buffer
	if err := Encode(&buf, want); err != nil {
		t.Fatal(err)
	}
	got, err := Decode(&buf)
	if err != nil || !reflect.DeepEqual(*got, want) {
		t.Fatalf("decoded %+v, %v", got, err)
	}
	if _, err = Decode(bytes.NewReader([]byte("not gzip"))); err == nil {
		t.Fatal("decoded a file that is not gzip")
	}
}
