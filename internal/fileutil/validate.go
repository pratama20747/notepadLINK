// Package fileutil menyediakan validasi tipe file yang tidak bisa dipercaya
// begitu saja dari klaim klien — mengecek magic bytes isi file yang
// sebenarnya tersimpan.
package fileutil

import (
	"errors"
	"net/http"
)

var AllowedImageTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
}

var AllowedVideoTypes = map[string]bool{
	"video/mp4":  true,
	"video/webm": true,
}

var ErrTypeNotAllowed = errors.New("tipe file tidak diizinkan")
var ErrTypeMismatch = errors.New("tipe file asli tidak sesuai dengan yang diklaim")

// DetectAndValidate mendeteksi tipe asli dari byte pertama file (magic bytes),
// membandingkan dengan whitelist DAN dengan content_type yang diklaim klien.
// data cukup beberapa ratus byte pertama saja (http.DetectContentType hanya
// butuh maksimal 512 byte).
func DetectAndValidate(data []byte, claimedType string, allowed map[string]bool) (actualType string, err error) {
	actualType = http.DetectContentType(data)

	if !allowed[actualType] {
		return actualType, ErrTypeNotAllowed
	}
	if claimedType != "" && claimedType != actualType {
		return actualType, ErrTypeMismatch
	}
	return actualType, nil
}

// ExtFromContentType mengembalikan ekstensi file dari content-type, dipakai
// untuk penamaan key di R2.
func ExtFromContentType(contentType string) string {
	switch contentType {
	case "image/jpeg":
		return "jpg"
	case "image/png":
		return "png"
	case "image/webp":
		return "webp"
	case "video/mp4":
		return "mp4"
	case "video/webm":
		return "webm"
	default:
		return "bin"
	}
}
