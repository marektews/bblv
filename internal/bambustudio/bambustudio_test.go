package bambustudio

import "testing"

func TestAccessCodes(t *testing.T) {
	conf := `{
	"access_code": {"03919D000000000": "12345678", "OTHER": ""},
	"app": {"language": "pl_PL"}
}
# MD5 checksum 0123456789ABCDEF
`
	codes, err := AccessCodes([]byte(conf))
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 1 || codes["03919D000000000"] != "12345678" {
		t.Fatalf("codes = %v", codes)
	}
	if _, err := AccessCodes([]byte("not json")); err == nil {
		t.Fatal("expected an error")
	}
}
