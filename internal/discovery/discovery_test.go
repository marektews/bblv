package discovery

import (
	"strings"
	"testing"
)

const notify = "NOTIFY * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nServer: UPnP/1.0\r\nLocation: 192.168.0.104\r\n" +
	"NT: urn:bambulab-com:device:3dprinter:1\r\nUSN: 03919D000000000\r\nCache-Control: max-age=1800\r\n" +
	"DevModel.bambu.com: N2S\r\nDevName.bambu.com: Bambus A1\r\nDevSignal.bambu.com: -46\r\n" +
	"DevConnect.bambu.com: cloud\r\nDevBind.bambu.com: occupied\r\nDevVersion.bambu.com: 01.08.01.00\r\n\r\n"

func TestParse(t *testing.T) {
	d, ok := Parse([]byte(notify))
	if !ok {
		t.Fatal("not parsed")
	}
	want := Device{Serial: "03919D000000000", Host: "192.168.0.104", Model: "A1", Code: "N2S", Name: "Bambus A1", Connect: "cloud", Firmware: "01.08.01.00"}
	if d != want {
		t.Fatalf("got %+v\nwant %+v", d, want)
	}
}

func TestParseRejectsOtherDevices(t *testing.T) {
	other := strings.Replace(notify, "urn:bambulab-com:device:3dprinter:1", "upnp:rootdevice", 1)
	if _, ok := Parse([]byte(other)); ok {
		t.Fatal("non-Bambu device accepted")
	}
	if _, ok := Parse([]byte("M-SEARCH * HTTP/1.1\r\n\r\n")); ok {
		t.Fatal("search request accepted")
	}
}

func TestParseLocationURL(t *testing.T) {
	d, ok := Parse([]byte(strings.Replace(notify, "Location: 192.168.0.104", "Location: http://192.168.0.7:80/desc.xml", 1)))
	if !ok || d.Host != "192.168.0.7" {
		t.Fatalf("host = %q", d.Host)
	}
}
