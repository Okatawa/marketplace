package listings

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

func TestMediaRejectsSpoofedMIME(t *testing.T) {
	if _, e := ValidateImage([]byte("<svg onload=alert(1)>")); e == nil {
		t.Fatal("accepted fake image")
	}
}
func TestMediaSizeLimit(t *testing.T) {
	if _, e := ValidateImage(make([]byte, 5*1024*1024+1)); e == nil {
		t.Fatal("accepted large image")
	}
}
func TestValidPNG(t *testing.T) {
	var b bytes.Buffer
	png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	if mime, e := ValidateImage(b.Bytes()); e != nil || mime != "image/png" {
		t.Fatal(mime, e)
	}
}
