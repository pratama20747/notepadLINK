// Package idgen menghasilkan ID unik pendek yang dipakai sebagai slug pada
// link share (mis. https://domain.com/n/ab).
//
// ID dibuat dari counter global sequential (tabel id_counter, lihat query
// GetNextCounter di internal/db/sqlc), di-encode dengan bijective numeration
// (skema yang sama seperti penamaan kolom spreadsheet: a, b, c, ..., z, aa,
// ab, ..., az, ba, ...) memakai alphabet di bawah.
//
// PERINGATAN: skema ini sequential & predictable. ID note PUBLIC (yang tidak
// dilindungi password) jadi bisa di-enumerasi dengan mudah (looping a, b, c,
// ...) tanpa perlu tahu ID-nya sama sekali. Kalau ini jadi concern, tambahkan
// rate limiting yang lebih ketat di endpoint GetNote, atau pertimbangkan
// permutasi tambahan (mis. Sqids/Feistel cipher) sebelum di-expose sebagai
// slug publik.
package idgen

// alphabet menentukan basis numerasi. Encode(1) menghasilkan karakter
// pertama alphabet, Encode(len(alphabet)+1) mulai menghasilkan 2 karakter,
// dst — sama seperti penamaan kolom Excel/Spreadsheet.
const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

var base = int64(len(alphabet))

// Encode mengubah counter (dimulai dari 1) menjadi slug bijective base-N.
//
//	Encode(1)  -> "a"
//	Encode(2)  -> "b"
//	...
//	Encode(62) -> "9"          (karakter terakhir alphabet)
//	Encode(63) -> "aa"
//	Encode(64) -> "ab"
//	...
//
// counter harus >= 1 (nilai 0 atau negatif dianggap tidak valid dan
// mengembalikan string kosong). Ini konsisten dengan GetNextCounter yang
// selalu meng-increment dulu sebelum mengembalikan nilai, sehingga counter
// pertama yang diterima dari DB adalah 1, bukan 0.
func Encode(counter int64) string {
	if counter <= 0 {
		return ""
	}

	var buf []byte
	for counter > 0 {
		// Bijective numeration: kurangi 1 dulu sebelum modulo, supaya tidak
		// ada "digit nol" yang ambigu (beda dengan base konversi biasa).
		counter--
		digit := counter % base
		buf = append(buf, alphabet[digit])
		counter /= base
	}

	// buf saat ini dari digit paling tidak signifikan ke paling signifikan,
	// perlu dibalik.
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}

	return string(buf)
}
