// Package cryptoutil menyediakan fungsi untuk menurunkan encryption key dari
// Argon2/ password (Argon2id) serta enkripsi/dekripsi konten (AES-256-GCM).
//
// Catatan desain: kita tidak menyimpan hash password secara terpisah.
// Verifikasi password dilakukan dengan cara mencoba men-decrypt konten yang
// tersimpan menggunakan key yang diturunkan dari password yang diberikan.
// Jika password salah, AES-GCM authentication tag akan gagal tervalidasi
// dan Decrypt akan mengembalikan ErrDecryptionFailed.
package cryptoutil

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

const (
	saltLength = 16
	keyLength  = 32 // AES-256
)

// ErrDecryptionFailed dikembalikan ketika dekripsi gagal, baik karena
// password salah maupun data yang korup.
var ErrDecryptionFailed = errors.New("dekripsi gagal: password salah atau data tidak valid")

// GenerateSalt membuat salt acak untuk key derivation.
func GenerateSalt() ([]byte, error) {
	salt := make([]byte, saltLength)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, err
	}
	return salt, nil
}

// DeriveKey menurunkan encryption key 32-byte dari password + salt menggunakan Argon2id.
// Parameter (time=1, memory=64MB, threads=4) dipilih sebagai baseline yang wajar
// untuk MVP; sesuaikan lagi (naikkan memory/time) untuk produksi jika resource cukup.
func DeriveKey(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, 1, 64*1024, 4, keyLength)
}

// Encrypt mengenkripsi plaintext dengan AES-256-GCM.
// Hasil yang dikembalikan adalah nonce||ciphertext (nonce disimpan di depan
// agar Decrypt bisa mengambilnya kembali).
func Encrypt(plaintext []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	// Seal meng-append hasil enkripsi ke slice nonce, sehingga hasil akhirnya
	// adalah nonce||ciphertext.
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// Decrypt mendekripsi data hasil Encrypt. Mengembalikan ErrDecryptionFailed
// jika key (password) salah atau data korup.
func Decrypt(data []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, ErrDecryptionFailed
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	return plaintext, nil
}

// ErrWrongEditPassword dikembalikan ketika edit_password tidak cocok dengan hash.
var ErrWrongEditPassword = errors.New("edit_password salah")

// HashEditPassword menghasilkan bcrypt hash dari edit_password (dipakai untuk
// fitur view-only: password ini HANYA mengotorisasi update/delete, bukan
// untuk enkripsi apapun — jadi cukup bcrypt, tidak perlu Argon2id+AES).
func HashEditPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// VerifyEditPassword membandingkan edit_password dengan hash yang tersimpan.
func VerifyEditPassword(password, hash string) error {
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return ErrWrongEditPassword
	}
	return nil
}
