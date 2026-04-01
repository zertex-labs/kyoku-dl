package deezer

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"

	"golang.org/x/crypto/blowfish"
)

const (
	blowfishSecret = "g4el58wc0zvf9na1" // 16 bytes
	chunkSize      = 2048
	aesECBKey      = "jo6aey6haid2Teih" // for legacy URL construction
	cdnSeparator   = "\xa4"             // U+00A4 currency sign
)

var blowfishIV = []byte{0, 1, 2, 3, 4, 5, 6, 7}

// DeriveBlowfishKey computes the per-track Blowfish decryption key.
// The key is derived by XOR-ing halves of the track ID's MD5 hash
// with the Deezer blowfish secret.
func DeriveBlowfishKey(trackID string) []byte {
	hash := md5.Sum([]byte(trackID))
	idMD5 := hex.EncodeToString(hash[:]) // 32-char lowercase hex

	key := make([]byte, 16)
	for i := 0; i < 16; i++ {
		key[i] = idMD5[i] ^ idMD5[i+16] ^ blowfishSecret[i]
	}

	return key
}

// DecryptStream reads Blowfish-CBC-encrypted Deezer audio from reader
// and writes the decrypted output to writer.
//
// Deezer encrypts every 3rd 2048-byte chunk (i.e. chunks at index 0, 3, 6, ...).
// The CBC IV is reset to [0,1,2,3,4,5,6,7] for each encrypted chunk.
// Short chunks (< 2048 bytes, typically the final chunk) are always plaintext.
func DecryptStream(reader io.Reader, writer io.Writer, trackID string) error {
	key := DeriveBlowfishKey(trackID)

	bf, err := blowfish.NewCipher(key)
	if err != nil {
		return fmt.Errorf("failed to create blowfish cipher: %w", err)
	}

	buf := make([]byte, chunkSize)
	chunkIndex := 0

	for {
		n, readErr := io.ReadFull(reader, buf)
		if n == 0 {
			if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
				break
			}
			if readErr != nil {
				return fmt.Errorf("read error: %w", readErr)
			}
			break
		}

		chunk := buf[:n]

		if chunkIndex%3 == 0 && n == chunkSize {
			// Decrypt this chunk with Blowfish CBC, resetting IV each time
			iv := make([]byte, 8)
			copy(iv, blowfishIV)
			decrypter := cipher.NewCBCDecrypter(bf, iv)
			decrypter.CryptBlocks(chunk, chunk)
		}

		if _, err := writer.Write(chunk); err != nil {
			return fmt.Errorf("write error: %w", err)
		}

		chunkIndex++

		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
	}

	return nil
}

// BuildLegacyCDNURL constructs an encrypted legacy CDN URL for a Deezer track.
// This is the fallback when the modern media.deezer.com API fails.
func BuildLegacyCDNURL(md5Origin, sngID, mediaVersion, quality string) (string, error) {
	qualityCode := qualityToCode(quality)

	// Build plaintext: md5Origin + separator + qualityCode + separator + sngID + separator + mediaVersion
	plaintext := md5Origin + cdnSeparator + qualityCode + cdnSeparator + sngID + cdnSeparator + mediaVersion

	// Compute hash of plaintext
	hash := md5.Sum([]byte(plaintext))
	hashHex := hex.EncodeToString(hash[:])

	// Build filename: hash + separator + plaintext + separator
	filename := hashHex + cdnSeparator + plaintext + cdnSeparator

	// Pad to nearest multiple of 16 with spaces
	for len(filename)%16 != 0 {
		filename += " "
	}

	// Encrypt with AES-128-ECB
	encrypted, err := aesECBEncrypt([]byte(aesECBKey), []byte(filename))
	if err != nil {
		return "", fmt.Errorf("AES-ECB encryption failed: %w", err)
	}

	encHex := hex.EncodeToString(encrypted)

	// Build CDN URL using first char of md5Origin for proxy routing
	cdnURL := fmt.Sprintf("https://e-cdns-proxy-%c.dzcdn.net/mobile/1/%s", md5Origin[0], encHex)

	return cdnURL, nil
}

// qualityToCode maps Deezer quality names to numeric codes for CDN URLs.
func qualityToCode(quality string) string {
	switch quality {
	case "FLAC":
		return "9"
	case "MP3_320":
		return "3"
	case "MP3_128":
		return "1"
	default:
		return "1"
	}
}

// aesECBEncrypt encrypts data with AES-128-ECB (no padding — input must be
// a multiple of 16 bytes).
func aesECBEncrypt(key, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	if len(data)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("data length %d is not a multiple of AES block size %d", len(data), aes.BlockSize)
	}

	encrypted := make([]byte, len(data))
	for i := 0; i < len(data); i += aes.BlockSize {
		block.Encrypt(encrypted[i:i+aes.BlockSize], data[i:i+aes.BlockSize])
	}

	return encrypted, nil
}
