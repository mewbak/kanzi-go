/*
Copyright 2011-2017 Frederic Langlet
Licensed under the Apache License, Version 2.0 (the "License")
you may not use this file except in compliance with the License.
you may obtain a copy of the License at

                http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package function

import (
	"errors"
	"fmt"

	kanzi "github.com/flanglet/kanzi-go"
)

// Simple one-pass text codec. Uses a default (small) static dictionary
// or potentially larger custom one. Generates a dynamic dictionary.

const (
	TC_THRESHOLD1             = 128
	TC_THRESHOLD2             = TC_THRESHOLD1 * TC_THRESHOLD1
	TC_THRESHOLD3             = 32
	TC_THRESHOLD4             = TC_THRESHOLD3 * 128
	TC_MAX_DICT_SIZE          = 1 << 19    // must be less than 1<<24
	TC_MAX_WORD_LENGTH        = 32         // must be less than 128
	TC_LOG_HASHES_SIZE        = 24         // 16 MB
	TC_MAX_BLOCK_SIZE         = 1 << 30    // 1 GB
	TC_ESCAPE_TOKEN1          = byte(0x0F) // dictionary word preceded by space symbol
	TC_ESCAPE_TOKEN2          = byte(0x0E) // toggle upper/lower case of first word char
	LF                        = byte(0x0A)
	CR                        = byte(0x0D)
	TC_MASK_NOT_TEXT          = 0x80
	TC_MASK_ALMOST_FULL_ASCII = 0x08
	TC_MASK_FULL_ASCII        = 0x04
	TC_MASK_XML_HTML          = 0x02
	TC_MASK_CRLF              = 0x01
	TC_HASH1                  = int32(2146121005)  // 0x7FEB352D
	TC_HASH2                  = int32(-2073254261) // 0x846CA68B
)

type dictEntry struct {
	hash int32  // full word hash
	data int32  // packed word length (8 MSB) + index in dictionary (24 LSB)
	ptr  []byte // text data
}

type TextCodec struct {
	delegate kanzi.ByteFunction
}

type textCodec1 struct {
	dictMap        []*dictEntry
	dictList       []dictEntry
	staticDictSize int
	dictSize       int
	logHashSize    uint
	hashMask       int32
	isCRLF         bool // EOL = CR+LF ?
}

type textCodec2 struct {
	dictMap        []*dictEntry
	dictList       []dictEntry
	staticDictSize int
	dictSize       int
	logHashSize    uint
	hashMask       int32
	isCRLF         bool // EOL = CR+LF ?
}

var (
	TC_STATIC_DICTIONARY = make([]dictEntry, 1024)
	TC_STATIC_DICT_WORDS = createDictionary(unpackDictionary32(TC_DICT_EN_1024), TC_STATIC_DICTIONARY, 1024, 0)
	TC_DELIMITER_CHARS   = initDelimiterChars()

	// Default dictionary
	// 1024 of the most common English words with at least 2 chars.
	// Each char is 6 bit encoded: 0 to 31. Add 32 to a letter starting a word (MSB).
	// TheBeAndOfInToHaveItThatFor...
	TC_DICT_EN_1024 = []byte{
		byte(0xCC), byte(0x71), byte(0x21), byte(0x12), byte(0x03), byte(0x43), byte(0xB8), byte(0x5A),
		byte(0x0D), byte(0xCC), byte(0xED), byte(0x88), byte(0x4C), byte(0x7A), byte(0x13), byte(0xCC),
		byte(0x70), byte(0x13), byte(0x94), byte(0xE4), byte(0x78), byte(0x39), byte(0x49), byte(0xC4),
		byte(0x9C), byte(0x05), byte(0x44), byte(0xB8), byte(0xDC), byte(0x80), byte(0x20), byte(0x3C),
		byte(0x80), byte(0x62), byte(0x04), byte(0xE1), byte(0x51), byte(0x3D), byte(0x84), byte(0x85),
		byte(0x89), byte(0xC0), byte(0x0F), byte(0x31), byte(0xC4), byte(0x62), byte(0x04), byte(0xB6),
		byte(0x39), byte(0x42), byte(0xC3), byte(0xD8), byte(0x73), byte(0xAE), byte(0x46), byte(0x20),
		byte(0x0D), byte(0xB0), byte(0x06), byte(0x23), byte(0x3B), byte(0x31), byte(0xC8), byte(0x4B),
		byte(0x60), byte(0x12), byte(0xA1), byte(0x2B), byte(0x14), byte(0x08), byte(0x78), byte(0x0D),
		byte(0x62), byte(0x54), byte(0x4E), byte(0x32), byte(0xD3), byte(0x93), byte(0xC8), byte(0x71),
		byte(0x36), byte(0x1C), byte(0x04), byte(0xF3), byte(0x1C), byte(0x42), byte(0x11), byte(0xD8),
		byte(0x72), byte(0x02), byte(0x1E), byte(0x61), byte(0x13), byte(0x98), byte(0x85), byte(0x44),
		byte(0x9C), byte(0x04), byte(0xA0), byte(0x44), byte(0x49), byte(0xC8), byte(0x32), byte(0x71),
		byte(0x11), byte(0x88), byte(0xE3), byte(0x04), byte(0xB1), byte(0x8B), byte(0x94), byte(0x47),
		byte(0x61), byte(0x11), byte(0x13), byte(0x62), byte(0x0B), byte(0x2F), byte(0x23), byte(0x8C),
		byte(0x12), byte(0x11), byte(0x02), byte(0x01), byte(0x44), byte(0x84), byte(0xCC), byte(0x71),
		byte(0x11), byte(0x13), byte(0x31), byte(0xD1), byte(0x39), byte(0x41), byte(0x87), byte(0xCC),
		byte(0x42), byte(0xCB), byte(0xD8), byte(0x71), byte(0x0D), byte(0xD8), byte(0xE4), byte(0x4A),
		byte(0xCC), byte(0x71), byte(0x0C), byte(0xE0), byte(0x44), byte(0xF4), byte(0x3E), byte(0xE5),
		byte(0x8D), byte(0xB9), byte(0x44), byte(0xE8), byte(0x35), byte(0x33), byte(0xA9), byte(0x51),
		byte(0x24), byte(0xE2), byte(0x39), byte(0x42), byte(0xC3), byte(0xB9), byte(0x51), byte(0x11),
		byte(0xB8), byte(0xB0), byte(0xF3), byte(0x1C), byte(0x83), byte(0x4A), byte(0x8C), byte(0x06),
		byte(0x36), byte(0x01), byte(0x8C), byte(0xC7), byte(0x00), byte(0xDA), byte(0xC8), byte(0x28),
		byte(0x4B), byte(0x93), byte(0x1C), byte(0x44), byte(0x67), byte(0x39), byte(0x6C), byte(0xC7),
		byte(0x10), byte(0xDA), byte(0x13), byte(0x4A), byte(0xF1), byte(0x0E), byte(0x3C), byte(0xB1),
		byte(0x33), byte(0x58), byte(0xEB), byte(0x0E), byte(0x44), byte(0x4C), byte(0xC7), byte(0x11),
		byte(0x21), byte(0x21), byte(0x10), byte(0x43), byte(0x6D), byte(0x39), byte(0x6D), byte(0x80),
		byte(0x35), byte(0x39), byte(0x48), byte(0x45), byte(0x24), byte(0xED), byte(0x11), byte(0x6D),
		byte(0x12), byte(0x13), byte(0x21), byte(0x04), byte(0xCC), byte(0x83), byte(0x04), byte(0xB0),
		byte(0x03), byte(0x6C), byte(0x00), byte(0xD6), byte(0x33), byte(0x1C), byte(0x83), byte(0x46),
		byte(0xB0), byte(0x02), byte(0x84), byte(0x9C), byte(0x44), byte(0x44), byte(0xD8), byte(0x42),
		byte(0xCB), byte(0xB8), byte(0xD2), byte(0xD8), byte(0x9C), byte(0x84), byte(0xB5), byte(0x11),
		byte(0x16), byte(0x20), byte(0x15), byte(0x31), byte(0x11), byte(0xD8), byte(0x84), byte(0xC7),
		byte(0x39), byte(0x44), byte(0xE0), byte(0x34), byte(0xE4), byte(0xC7), byte(0x11), byte(0x1B),
		byte(0x4E), byte(0x80), byte(0xB2), byte(0xE1), byte(0x10), byte(0xB2), byte(0x04), byte(0x54),
		byte(0x48), byte(0x44), byte(0x14), byte(0xE4), byte(0x44), byte(0xB8), byte(0x51), byte(0x73),
		byte(0x1C), byte(0xE5), byte(0x06), byte(0x1F), byte(0x23), byte(0xA0), byte(0x18), byte(0x02),
		byte(0x0D), byte(0x49), byte(0x3D), byte(0x87), byte(0x20), byte(0xB1), byte(0x2B), byte(0x01),
		byte(0x24), byte(0xF3), byte(0x38), byte(0xE8), byte(0xCE), byte(0x58), byte(0xDC), byte(0xCE),
		byte(0x0C), byte(0x06), byte(0x32), byte(0x00), byte(0xC1), byte(0x21), byte(0x00), byte(0x22),
		byte(0xB3), byte(0x00), byte(0xA1), byte(0x24), byte(0x00), byte(0x21), byte(0xE3), byte(0x20),
		byte(0x51), byte(0x44), byte(0x44), byte(0x43), byte(0x53), byte(0xD8), byte(0x71), byte(0x11),
		byte(0x12), byte(0x11), byte(0x13), byte(0x58), byte(0x41), byte(0x0D), byte(0xCC), byte(0x73),
		byte(0x92), byte(0x12), byte(0x45), byte(0x44), byte(0x37), byte(0x21), byte(0x04), byte(0x37),
		byte(0x43), byte(0x43), byte(0x11), byte(0x18), byte(0x01), byte(0x39), byte(0x44), byte(0xEE),
		byte(0x34), byte(0x48), byte(0x0B), byte(0x48), byte(0xE9), byte(0x40), byte(0x09), byte(0x3B),
		byte(0x14), byte(0x49), byte(0x38), byte(0x02), byte(0x4D), byte(0x40), byte(0x0B), byte(0x2D),
		byte(0x8B), byte(0xD1), byte(0x11), byte(0x51), byte(0x0D), byte(0x4E), byte(0x45), byte(0xCF),
		byte(0x10), byte(0x24), byte(0xE2), byte(0x38), byte(0xD4), byte(0xC0), byte(0x20), byte(0xD8),
		byte(0x8E), byte(0x34), byte(0x21), byte(0x11), byte(0x36), byte(0xC1), byte(0x32), byte(0x08),
		byte(0x73), byte(0x8E), byte(0x2F), byte(0x81), byte(0x00), byte(0x47), byte(0x32), byte(0x0F),
		byte(0xAC), byte(0x00), byte(0x63), byte(0x50), byte(0x49), byte(0x15), byte(0x11), byte(0x1C),
		byte(0xCE), byte(0x58), byte(0x04), byte(0x43), byte(0x98), byte(0x84), byte(0x4B), byte(0x94),
		byte(0x84), byte(0x4C), byte(0x98), byte(0xB0), byte(0x12), byte(0x4A), byte(0x60), byte(0x12),
		byte(0xA8), byte(0x41), byte(0x0F), byte(0xD8), byte(0xE4), byte(0x4B), byte(0x0F), byte(0x24),
		byte(0xC8), byte(0x2C), byte(0xBD), byte(0x84), byte(0x35), byte(0x3C), byte(0x87), byte(0x39),
		byte(0x42), byte(0xC3), byte(0xC8), byte(0xF1), byte(0x0D), byte(0x0F), byte(0x24), byte(0xC0),
		byte(0x18), byte(0x48), byte(0xCE), byte(0x09), byte(0x33), byte(0x91), byte(0xB0), byte(0x81),
		byte(0x87), byte(0x4E), byte(0x93), byte(0x81), byte(0x98), byte(0xE8), byte(0x8E), byte(0x35),
		byte(0x32), byte(0x0D), byte(0x50), byte(0x49), byte(0x15), byte(0x11), byte(0x16), byte(0x0E),
		byte(0x34), byte(0x4B), byte(0x44), byte(0x54), byte(0x44), byte(0x60), byte(0x35), byte(0x25),
		byte(0x84), byte(0x46), byte(0x51), byte(0x16), byte(0xB0), byte(0x40), byte(0x0D), byte(0x8C),
		byte(0x81), byte(0x45), byte(0x11), byte(0x11), byte(0x0D), byte(0x08), byte(0x4C), byte(0xC4),
		byte(0x34), byte(0x3B), byte(0x44), byte(0x10), byte(0x3A), byte(0xC4), byte(0x01), byte(0x51),
		byte(0x33), byte(0x45), byte(0x8B), byte(0x48), byte(0x08), byte(0x49), byte(0xCE), byte(0x2C),
		byte(0x3C), byte(0x8E), byte(0x30), byte(0x44), byte(0xC7), byte(0x20), byte(0xD1), byte(0xA0),
		byte(0x48), byte(0xAD), byte(0x80), byte(0x44), byte(0xCA), byte(0xC8), byte(0x3E), byte(0x23),
		byte(0x95), byte(0x11), byte(0x1A), byte(0x12), byte(0x49), byte(0x41), byte(0x27), byte(0x00),
		byte(0xF3), byte(0xC4), byte(0x37), byte(0x35), byte(0x11), byte(0x36), byte(0xB3), byte(0x8E),
		byte(0x2B), byte(0x25), byte(0x11), byte(0x12), byte(0x32), byte(0x12), byte(0x08), byte(0xE5),
		byte(0x44), byte(0x46), byte(0x52), byte(0x06), byte(0x1D), byte(0x3B), byte(0x00), byte(0x0E),
		byte(0x32), byte(0x11), byte(0x10), byte(0x24), byte(0xC8), byte(0x38), byte(0xD8), byte(0x06),
		byte(0x44), byte(0x41), byte(0x32), byte(0x38), byte(0xC1), byte(0x0E), byte(0x34), byte(0x49),
		byte(0x40), byte(0x20), byte(0xBC), byte(0x44), byte(0x48), byte(0xF1), byte(0x02), byte(0x4E),
		byte(0xD3), byte(0x93), byte(0x20), byte(0x21), byte(0x22), byte(0x1C), byte(0xE2), byte(0x02),
		byte(0x12), byte(0x11), byte(0x06), byte(0x20), byte(0xDC), byte(0xC7), byte(0x44), byte(0x41),
		byte(0x32), byte(0x61), byte(0x24), byte(0xC4), byte(0x32), byte(0xB1), byte(0x15), byte(0x10),
		byte(0xB9), byte(0x44), byte(0x10), byte(0xBB), byte(0x04), byte(0x11), byte(0x38), byte(0x8E),
		byte(0x30), byte(0xF0), byte(0x0D), byte(0x62), byte(0x13), byte(0x97), byte(0xC8), byte(0x73),
		byte(0x96), byte(0xBC), byte(0xB0), byte(0x18), byte(0xAC), byte(0x85), byte(0x44), byte(0xAC),
		byte(0x44), byte(0xD3), byte(0x11), byte(0x19), byte(0x06), byte(0x1A), byte(0xD5), byte(0x0C),
		byte(0x04), byte(0x44), byte(0x6E), byte(0x3C), byte(0x43), byte(0x6F), byte(0x44), byte(0xE0),
		byte(0x4B), byte(0x10), byte(0xC9), byte(0x40), byte(0x4E), byte(0x70), byte(0x0D), byte(0x0E),
		byte(0xC1), byte(0x00), byte(0x49), byte(0x44), byte(0x44), byte(0xC1), byte(0x41), byte(0x12),
		byte(0x4C), byte(0x83), byte(0x8D), byte(0x88), byte(0x02), byte(0xCB), byte(0xC4), byte(0x43),
		byte(0x04), byte(0x30), byte(0x11), byte(0x11), byte(0x88), byte(0x44), byte(0x53), byte(0x00),
		byte(0x83), byte(0x6F), byte(0x51), byte(0x3B), byte(0x44), byte(0x5D), byte(0x38), byte(0x87),
		byte(0x00), byte(0x84), byte(0x72), byte(0x4C), byte(0x04), byte(0x53), byte(0xC5), byte(0x43),
		byte(0x71), byte(0x00), byte(0x84), byte(0x84), byte(0x98), byte(0xE0), byte(0x0B), byte(0xC4),
		byte(0x40), byte(0x0B), byte(0x2D), byte(0x89), byte(0xCE), byte(0x30), byte(0x4C), byte(0xC4),
		byte(0x02), byte(0x20), byte(0x0D), byte(0x0C), byte(0x80), byte(0xC0), byte(0x4C), byte(0x4B),
		byte(0x0E), byte(0x34), byte(0x46), byte(0x21), byte(0x51), byte(0x22), byte(0x0D), byte(0x11),
		byte(0x24), byte(0xB8), byte(0x39), byte(0x43), byte(0x46), byte(0x98), byte(0xE3), byte(0x83),
		byte(0x88), byte(0xE5), byte(0x11), byte(0x4E), byte(0x52), byte(0x0D), byte(0x0E), byte(0xA3),
		byte(0x4E), byte(0x5A), byte(0xA2), byte(0x0D), byte(0x0E), byte(0x71), byte(0x0B), byte(0x3E),
		byte(0xD2), byte(0x06), byte(0x1D), byte(0x38), byte(0x87), byte(0x20), byte(0xB0), byte(0xEB),
		byte(0x39), byte(0x3E), byte(0x0E), byte(0x51), byte(0x1D), byte(0x12), byte(0x91), byte(0x81),
		byte(0x38), byte(0x11), byte(0x2D), byte(0x8E), byte(0x44), byte(0x38), byte(0x48), byte(0x4F),
		byte(0x50), byte(0x0D), byte(0xB0), byte(0xE3), byte(0x53), byte(0x1E), byte(0x70), byte(0x0B),
		byte(0x16), byte(0xB3), byte(0x96), byte(0xB0), byte(0x82), byte(0xCB), byte(0x20), byte(0xE3),
		byte(0x67), byte(0x20), byte(0x61), byte(0xEE), byte(0x44), byte(0x60), byte(0x0D), byte(0x21),
		byte(0x90), byte(0x13), byte(0x20), byte(0xE3), byte(0x71), byte(0x10), byte(0x39), byte(0x91),
		byte(0x10), byte(0x43), byte(0x61), byte(0x2D), byte(0x41), byte(0x36), byte(0x1C), byte(0x84),
		byte(0xC4), byte(0x84), byte(0xB0), byte(0x02), byte(0x2B), byte(0x83), byte(0x94), byte(0x45),
		byte(0x21), byte(0x0B), byte(0x16), byte(0x42), byte(0x06), byte(0x1D), byte(0x38), byte(0x4E),
		byte(0x4C), byte(0x7A), byte(0xC8), byte(0x4D), byte(0x32), byte(0xC4), byte(0x9C), byte(0xE5),
		byte(0x12), byte(0x12), byte(0xB1), byte(0x13), byte(0x8C), byte(0x44), byte(0x8F), byte(0x21),
		byte(0x31), byte(0x2F), byte(0x44), byte(0xE5), byte(0x48), byte(0x0C), byte(0x4C), byte(0x84),
		byte(0x45), byte(0x52), byte(0x02), byte(0x12), byte(0x72), byte(0x0C), byte(0x48), byte(0x42),
		byte(0xC5), byte(0x95), byte(0x12), byte(0x04), byte(0x34), byte(0x38), byte(0xC4), byte(0x48),
		byte(0x24), byte(0x48), byte(0x04), byte(0x49), byte(0x40), byte(0x4C), byte(0x71), byte(0x11),
		byte(0x8C), byte(0x45), byte(0x44), byte(0x2C), byte(0xE3), byte(0xCC), byte(0x10), byte(0xD4),
		byte(0xE0), byte(0x58), byte(0x06), byte(0x2A), byte(0x20), byte(0xB2), byte(0xF3), byte(0x44),
		byte(0x83), byte(0xE7), byte(0x39), byte(0x44), byte(0x66), byte(0x00), byte(0xC1), byte(0x2E),
		byte(0x15), byte(0x31), byte(0x0D), byte(0xBC), byte(0xB0), byte(0x0D), byte(0x4E), byte(0xF2),
		byte(0xC0), byte(0x08), byte(0x49), byte(0x0D), byte(0x0E), byte(0x03), byte(0x0E), byte(0x34),
		byte(0x6C), byte(0x88), byte(0x34), byte(0x21), byte(0x32), byte(0x4C), byte(0x03), byte(0x43),
		byte(0x8C), byte(0x44), byte(0x88), byte(0x18), byte(0xDB), byte(0xC0), byte(0x45), byte(0x32),
		byte(0x02), byte(0x50), byte(0xB0), byte(0x11), byte(0xC9), byte(0x40), byte(0xC3), byte(0x10),
		byte(0xD2), byte(0xD8), byte(0xB0), byte(0x43), byte(0x01), byte(0x11), byte(0x1B), byte(0xC0),
		byte(0x62), byte(0xB0), byte(0x16), byte(0x84), byte(0xE3), byte(0x8A), byte(0xC8), byte(0x82),
		byte(0xC4), byte(0x34), byte(0x21), byte(0x20), byte(0x2C), byte(0xC3), byte(0x92), byte(0x4E),
		byte(0x83), byte(0x42), byte(0x2D), byte(0x40), byte(0xC4), byte(0x80), byte(0x60), byte(0x08),
		byte(0x36), byte(0x42), byte(0x13), byte(0x1C), byte(0x44), byte(0x73), byte(0x38), byte(0xE2),
		byte(0xE5), byte(0x21), byte(0x51), byte(0x2E), byte(0x34), byte(0x21), byte(0x2B), byte(0x10),
		byte(0x04), byte(0x93), byte(0x91), byte(0x73), byte(0xCB), byte(0x00), byte(0x83), byte(0x68),
		byte(0x0C), byte(0x43), byte(0x53), byte(0x20), byte(0x56), byte(0x34), byte(0x35), byte(0x32),
		byte(0x0B), byte(0xC8), byte(0x84), byte(0xC4), byte(0xB0), byte(0x83), byte(0x54), byte(0x4C),
		byte(0x48), byte(0x8E), byte(0x50), byte(0xF2), byte(0xC4), byte(0xD8), byte(0x41), byte(0x0A),
		byte(0xB0), byte(0x04), byte(0xD3), byte(0x11), byte(0x18), byte(0x51), byte(0x20), byte(0xD1),
		byte(0xA3), byte(0x11), byte(0x30), byte(0x08), byte(0x2E), byte(0x83), byte(0x45), byte(0x39),
		byte(0x13), byte(0x00), byte(0x4C), byte(0x83), byte(0x8D), byte(0xB4), byte(0xE4), byte(0xC7),
		byte(0x20), byte(0xD1), byte(0xA0), byte(0x35), byte(0x84), byte(0xC7), byte(0x20), byte(0xD1),
		byte(0xA4), byte(0x54), byte(0x44), byte(0x58), byte(0x4C), byte(0x72), byte(0x0D), byte(0x1A),
		byte(0x01), byte(0x8E), byte(0xAC), byte(0x40), byte(0x03), byte(0xC8), byte(0xE3), byte(0x04),
		byte(0x4C), byte(0x83), byte(0x04), byte(0x4B), byte(0x43), byte(0x43), byte(0x11), byte(0x14),
		byte(0x93), byte(0x00), byte(0xD0), byte(0xF6), byte(0x1C), byte(0x44), byte(0xC7), byte(0x11),
		byte(0x1B), byte(0x40), byte(0x4D), byte(0x44), byte(0x44), byte(0xCC), byte(0xE1), byte(0x84),
		byte(0x4C), byte(0x71), byte(0x11), byte(0x94), byte(0xE2), byte(0xCB), byte(0x39), byte(0x6B),
		byte(0xC0), byte(0x44), byte(0x43), byte(0x53), byte(0xC9), byte(0x33), byte(0x8F), byte(0xA0),
		byte(0xD0), byte(0xC4), byte(0x10), byte(0x38), byte(0xC8), byte(0x14), byte(0x52), byte(0x02),
		byte(0x50), byte(0xB4), byte(0xEF), byte(0x50), byte(0x12), byte(0xC8), byte(0x0A), byte(0x02),
		byte(0xD1), byte(0x10), byte(0x00), byte(0xD8), byte(0xC8), byte(0xF1), byte(0x00), byte(0x2A),
		byte(0xC0), byte(0x08), byte(0x35), byte(0x30), byte(0x08), byte(0x37), byte(0x11), byte(0x0C),
		byte(0x00), byte(0x83), byte(0x67), byte(0x10), byte(0x04), byte(0x60), byte(0x2C), byte(0xB3),
		byte(0x96), byte(0xB0), byte(0x40), byte(0xC8), byte(0x02), byte(0xE1), byte(0x45), byte(0x20),
		byte(0x21), byte(0x21), byte(0x10), byte(0xD1), byte(0x05), byte(0x21), byte(0x38), byte(0xCE),
		byte(0x39), byte(0x19), byte(0xD4), byte(0x1A), byte(0xF1), byte(0x11), byte(0x48), byte(0xE3),
		byte(0x6B), byte(0x01), byte(0x31), byte(0x11), byte(0x8D), byte(0x44), byte(0x48), byte(0x34),
		byte(0x6D), byte(0x80), byte(0x46), byte(0x72), byte(0x12), byte(0x4C), byte(0xE4), byte(0x58),
		byte(0x81), byte(0x11), byte(0x94), byte(0x13), byte(0x62), byte(0x13), byte(0x1C), byte(0x83),
		byte(0x72), byte(0x11), byte(0x38), byte(0x11), byte(0x4C), byte(0x80), byte(0x8B), byte(0x13),
		byte(0x24), byte(0xC0), byte(0x4C), byte(0x83), byte(0x8D), byte(0xB0), byte(0xE4), byte(0x4D),
		byte(0x20), byte(0xD1), byte(0xB6), byte(0x00), byte(0xB2), byte(0xA4), byte(0x54), byte(0x43),
		byte(0x53), byte(0xD8), byte(0x83), byte(0x62), byte(0x1C), byte(0xE3), byte(0x92), byte(0x12),
		byte(0x11), byte(0x07), byte(0x01), byte(0x52), byte(0x0E), byte(0x47), byte(0x21), byte(0xCE),
		byte(0x39), byte(0x39), byte(0x48), byte(0x44), byte(0x49), byte(0x4E), byte(0x38), byte(0x3C),
		byte(0xC8), byte(0x4C), byte(0xB1), byte(0x20), byte(0x44), byte(0xE5), byte(0x0D), byte(0x0E),
		byte(0x02), byte(0x11), byte(0xCC), byte(0x40), byte(0x02), byte(0x1C), byte(0x44), byte(0x66),
		byte(0x00), byte(0xFC), byte(0x94), byte(0x04), byte(0x91), byte(0x02), byte(0x4E), byte(0x43),
		byte(0x4E), byte(0x50), byte(0x61), byte(0xEF), byte(0x44), byte(0xE5), byte(0x44), byte(0x80),
		byte(0x24), byte(0x4E), byte(0x49), byte(0x28), byte(0x0B), byte(0x4C), byte(0x73), byte(0x94),
		byte(0x18), byte(0x79), byte(0xC4), byte(0x00), byte(0x39), byte(0x4E), byte(0x39), byte(0x3C),
		byte(0x84), byte(0x08), byte(0xE3), byte(0x43), byte(0x84), byte(0xE6), byte(0x2C), byte(0x00),
		byte(0x83), byte(0x6B), byte(0x20), byte(0x48), byte(0x01), byte(0x2C), byte(0x48), byte(0x88),
		byte(0x54), byte(0x82), byte(0xF3), byte(0x00), byte(0x12), byte(0xC4), byte(0xAC), byte(0xE5),
		byte(0x44), byte(0xBD), byte(0x13), byte(0x82), byte(0x11), byte(0x24), byte(0xAE), byte(0x14),
		byte(0x51), byte(0x11), byte(0xC9), byte(0x35), byte(0x03), byte(0x10), byte(0xD4), byte(0xE2),
		byte(0x38), byte(0xD4), byte(0x88), byte(0x0C), byte(0x44), byte(0x60), byte(0x3C), byte(0xF1),
		byte(0x00), byte(0x47), byte(0x24), byte(0xD4), byte(0x0D), byte(0x88), byte(0x54), byte(0x62),
		byte(0xD1), byte(0x00), byte(0x44), byte(0xB6), byte(0x27), byte(0x50), byte(0xC0), byte(0x0D),
		byte(0x91), byte(0x52), byte(0x03), byte(0x10), byte(0xD0), byte(0x84), byte(0xCC), byte(0x45),
		byte(0xD3), byte(0xB0), byte(0x44), byte(0xC7), byte(0x38), byte(0x3A), byte(0x0D), byte(0x08),
		byte(0xB5), byte(0x03), byte(0x20), byte(0xD1), byte(0xB2), byte(0x10), byte(0xD0), byte(0xF1),
		byte(0x10), byte(0x02), byte(0xC8), byte(0x64), byte(0x4C), byte(0x84), byte(0x35), byte(0x21),
		byte(0x21), byte(0x50), byte(0x82), byte(0xC3), byte(0x88), byte(0xE3), byte(0x53), byte(0x44),
		byte(0xE2), byte(0xE0), byte(0x50), byte(0x32), byte(0x04), byte(0x34), byte(0x21), byte(0x32),
		byte(0x11), byte(0x51), byte(0x11), byte(0x00), byte(0xB8), byte(0x94), byte(0x4E), byte(0x23),
		byte(0x8B), byte(0x2C), byte(0x41), byte(0x84), byte(0xA0), byte(0xD4), byte(0xC4), byte(0x44),
		byte(0x44), byte(0x93), byte(0xC9), byte(0x40), byte(0x82), byte(0x11), byte(0x24), byte(0xB2),
		byte(0x3C), byte(0x40), byte(0x88), byte(0x00), byte(0xBC), byte(0x48), byte(0x48), byte(0xA9),
		byte(0x17), byte(0x3C), byte(0x44), byte(0x48), byte(0x10), byte(0xD0), byte(0x84), byte(0x84),
		byte(0x41), byte(0xC8), byte(0x34), byte(0x38), byte(0x44), byte(0x4D), byte(0x31), byte(0x11),
		byte(0xC4), byte(0x44), byte(0x94), byte(0x2D), byte(0x3C), byte(0xD1), byte(0x10), byte(0x04),
		byte(0xF2), byte(0x21), byte(0x7C), byte(0x44), byte(0x2C), byte(0x04), byte(0xC8), byte(0x38),
		byte(0xD4), byte(0x87), byte(0x20), byte(0xF8), byte(0x0D), byte(0x20), byte(0xC0), byte(0x0B),
		byte(0xA0), byte(0xC3), byte(0xD1), byte(0x39), byte(0x51), byte(0x27), byte(0x00), byte(0x84),
		byte(0x72), byte(0x4C), byte(0x06), byte(0x33), byte(0x38), byte(0xFC), byte(0x44), byte(0x0D),
		byte(0x40), byte(0x84), byte(0xBC), byte(0x44), byte(0x47), byte(0x00), byte(0xF4), byte(0xAB),
		byte(0x01), byte(0x31), byte(0x36), byte(0x44), byte(0x84), byte(0xC4), byte(0x46), byte(0xF2),
		byte(0x02), byte(0x2A), byte(0x42), byte(0xD2), byte(0x13), byte(0x22), byte(0x06), byte(0x34),
		byte(0x81), byte(0x48), byte(0x08), byte(0x03), byte(0x53), byte(0x88), byte(0x70), byte(0x0D),
		byte(0x08), byte(0x49), byte(0xCE), byte(0x4C), byte(0x42), byte(0xE6), byte(0x10), byte(0xD1),
		byte(0x11), byte(0x00), byte(0xBC), byte(0x4E), byte(0x08), byte(0xAC), byte(0x44), byte(0x41),
		byte(0x42), byte(0x11), byte(0x12), byte(0x02), byte(0xCE), byte(0x34), byte(0x69), byte(0x48),
		byte(0x4F), byte(0x31), byte(0xC4), byte(0x31), byte(0x21), byte(0x0B), byte(0x54), byte(0x44),
		byte(0xB1), byte(0x10), byte(0xF3), byte(0x91), byte(0x4E), byte(0x23), byte(0x8D), byte(0x0C),
		byte(0x84), byte(0xC8), byte(0x38), byte(0xDC), byte(0x44), byte(0x00), byte(0x21), byte(0xF3),
		byte(0x45), byte(0x44), byte(0xC7), byte(0x90), byte(0x51), byte(0x4E), byte(0x45), byte(0x38),
		byte(0xC4), byte(0x08), byte(0x80), byte(0xC4), byte(0xC4), byte(0x04), byte(0xC4), byte(0x90),
		byte(0x35), byte(0x02), byte(0x01), byte(0x32), byte(0x0E), byte(0x36), byte(0x53), byte(0x91),
		byte(0x08), byte(0x49), byte(0x80), byte(0x44), byte(0x31), byte(0x0D), byte(0x8D), byte(0x15),
		byte(0x06), byte(0xAC), byte(0x40), byte(0x03), byte(0x11), byte(0x1D), byte(0x4E), byte(0x20),
		byte(0x21), byte(0x30), byte(0x50), byte(0x84), byte(0xC4), byte(0xD8), byte(0x73), byte(0x8B),
		byte(0x13), byte(0x21), byte(0x04), byte(0x32), byte(0xC2), byte(0x0D), byte(0x0E), byte(0x52),
		byte(0x0D), byte(0x00), byte(0xB2), byte(0xD8), byte(0xC8), byte(0x84), byte(0x71), byte(0x11),
		byte(0x35), byte(0x11), byte(0x36), byte(0x54), byte(0x44), byte(0x13), byte(0x24), byte(0xCE),
		byte(0x45), byte(0x8C), byte(0x44), byte(0x48), byte(0xF3), byte(0x8D), byte(0x0E), byte(0xF5),
		byte(0x12), byte(0x1E), byte(0x00), byte(0x82), byte(0x39), byte(0x10), byte(0xC8), byte(0x34),
		byte(0x68), byte(0x51), byte(0x39), byte(0x31), byte(0xC4), byte(0x46), byte(0xB1), byte(0x00),
		byte(0x44), byte(0xDC), byte(0x8E), byte(0x36), byte(0x73), byte(0x8F), byte(0x12), byte(0x31),
		byte(0x15), byte(0x10), byte(0xB3), byte(0x8F), byte(0x94), byte(0x41), byte(0x0B), byte(0x20),
		byte(0xD1), byte(0xB1), byte(0x10), byte(0x00), byte(0xE2), byte(0x01), byte(0x14), byte(0x58),
		byte(0x8C), byte(0x84), byte(0x84), byte(0x01), byte(0x21), byte(0x31), byte(0x38), byte(0x00),
		byte(0xF5), byte(0x01), byte(0x12), byte(0x0E), byte(0x51), byte(0x28), byte(0x40), byte(0x2C),
		byte(0xB8), byte(0x80), byte(0x48), byte(0x4B), byte(0x8F), byte(0x11), byte(0x10), byte(0x13),
		byte(0x20), byte(0xE3), byte(0x62), byte(0x2C), byte(0xE4), byte(0x84), byte(0xD4), byte(0x84),
		byte(0x88), byte(0x4F), byte(0x11), byte(0x02), byte(0x10), byte(0x85), byte(0x44), byte(0x85),
		byte(0x42), byte(0x0B), byte(0x0C), byte(0x83), byte(0x46), byte(0xD4), byte(0x02), byte(0xD4),
		byte(0x13), byte(0x11), byte(0x12), byte(0x10), byte(0x04), byte(0x42), byte(0x1E), byte(0x55),
		byte(0x0B), byte(0x2E), byte(0xC3), byte(0x83), byte(0x10), byte(0xBA), byte(0x4E), byte(0x20),
		byte(0xDC), byte(0x84), byte(0x01), byte(0x23), byte(0x8D), byte(0xCC), byte(0x05), byte(0xE3),
		byte(0x21), byte(0x11), byte(0x02), byte(0x4C), byte(0xE4), byte(0x6F), byte(0x39), byte(0x22),
		byte(0x13), byte(0x20), byte(0xE3), byte(0x6F), byte(0x2C), byte(0x06), byte(0x04), byte(0x47),
		byte(0x23), byte(0xCE), byte(0x45), byte(0x39), byte(0x11), byte(0x44), byte(0xE4), byte(0x71),
		byte(0x10), byte(0x23), byte(0x91), byte(0x0F), byte(0x13), byte(0x96), byte(0x8C), byte(0x04),
		byte(0xC0), byte(0xBC), byte(0x03), byte(0xC4), byte(0x47), byte(0x31), byte(0xC4), byte(0x39),
		byte(0x16), byte(0x32), byte(0x3C), byte(0x00), byte(0x84), byte(0x91), byte(0x51), byte(0x11),
		byte(0x62), byte(0x53), byte(0x91), byte(0x33), byte(0x25), byte(0x0F), byte(0x3C), byte(0xE4),
		byte(0x53), byte(0x80), byte(0x24), byte(0xC8), byte(0x38), byte(0xDB), byte(0x85), byte(0x14),
		byte(0x80), byte(0x88), byte(0x00), byte(0xBD), byte(0x87), byte(0x39), byte(0x21), byte(0x28),
		byte(0x0C), byte(0x40), byte(0x27), byte(0x00), byte(0xF3), byte(0xD8), byte(0x9C), byte(0x40),
		byte(0x11), byte(0x4E), byte(0x11), byte(0x12), byte(0x4F), byte(0x31), byte(0x00), byte(0x32),
		byte(0xF4), byte(0x4E), byte(0x24), byte(0x40), byte(0x93), byte(0x9C), byte(0x84), byte(0xE1),
		byte(0x01), byte(0x21), byte(0x31), byte(0x10), byte(0xF4), byte(0x44), byte(0x48), byte(0x43),
		byte(0x53), byte(0xCC), byte(0xE5), byte(0x8D), byte(0xBD), byte(0x42), byte(0xCB), byte(0x85),
		byte(0x44), byte(0xAC), byte(0x00), byte(0xF8), byte(0xD1), byte(0x62), byte(0xC3), byte(0x8C),
		byte(0x88), byte(0x04), byte(0xE3), byte(0x00), byte(0x3C), byte(0x4E), byte(0x38), byte(0xCC),
		byte(0x8C), byte(0x20), byte(0xB1), byte(0x25), byte(0x20), byte(0x42), byte(0xC3), byte(0xA0),
		byte(0xC3), byte(0xC0), byte(0x09), byte(0x39), byte(0x54), byte(0x34), byte(0x3A), byte(0xC0),
		byte(0x44), byte(0x61), byte(0x23), byte(0x38), byte(0x69), byte(0xD4), byte(0x18), byte(0x4B),
		byte(0xD1), byte(0x10), byte(0xF0), byte(0x11), byte(0x12), byte(0x43), byte(0x55), byte(0x21),
		byte(0x13), byte(0x8D), byte(0x30), byte(0x43), byte(0x53), byte(0x00), byte(0xBB), byte(0xD1),
		byte(0x38), byte(0x35), byte(0x02), byte(0x12), byte(0x71), byte(0x11), byte(0x48), byte(0x42),
		byte(0xC5), byte(0xCC), byte(0x40), byte(0x02), byte(0x1E), byte(0xE2), byte(0x0B), byte(0xC9),
		byte(0x40), byte(0x87), byte(0xC8), byte(0x84), byte(0xD4), byte(0x01), byte(0x32), byte(0x0E),
		byte(0x37), byte(0x32), byte(0x04), byte(0x88), byte(0xE4), byte(0x93), byte(0xA0), byte(0xD0),
		byte(0xD4), byte(0x49), byte(0x34), byte(0x58), byte(0xC8), byte(0xA2), byte(0x0D), byte(0xC9),
		byte(0x34), byte(0x44), byte(0x11), byte(0x3A), byte(0x0C), byte(0x00), byte(0x61), byte(0x28),
		byte(0x4D), byte(0x21), byte(0x0B), byte(0x16), byte(0xF1), byte(0xCE), byte(0x34), byte(0x4B),
		byte(0xD1), byte(0x20), byte(0x21), byte(0x36), byte(0x10), byte(0x04), byte(0x6C), byte(0x39),
		byte(0x24), byte(0xF2), byte(0x50), byte(0xDC), byte(0x8E), byte(0x38), byte(0xD8), byte(0x8B),
		byte(0x10), byte(0x04), byte(0x6F), byte(0x44), byte(0x00), byte(0x93), byte(0x20), byte(0x21),
		byte(0x2F), byte(0x20), byte(0x40), byte(0x84), byte(0xD8), byte(0x02), byte(0x13), byte(0xC4),
		byte(0x40), byte(0x84), byte(0x35), byte(0x3A), byte(0x0C), byte(0x3C), byte(0xE4), byte(0x53),
		byte(0x00), byte(0xD4), byte(0xEF), byte(0x44), byte(0xE0), byte(0xD4), byte(0x09), byte(0x3A),
		byte(0xC4), byte(0x15), byte(0x3D), byte(0x80), byte(0x2C), byte(0xBC), byte(0x84), byte(0x44),
		byte(0x81), byte(0x12), byte(0xB4), byte(0x45), byte(0x92), byte(0xC8), byte(0x70), byte(0x11),
		byte(0x12), byte(0xC3), byte(0x95), byte(0x20), byte(0x4A), byte(0x88), byte(0x0E), byte(0xD3),
		byte(0x91), byte(0xC8), byte(0x83), byte(0x0F), byte(0x2D), byte(0x8D), byte(0x88), byte(0x14),
		byte(0x4B), byte(0x8D), byte(0x4C), byte(0xE8), byte(0x80), byte(0x4C), byte(0x21), byte(0xEC),
		byte(0x61), byte(0x21), byte(0x0B), byte(0x16), byte(0x52), byte(0x0D), byte(0x12), byte(0x23),
		byte(0x8C), byte(0x3D), byte(0x44), byte(0xC4), byte(0x47), byte(0x23), byte(0x8D), byte(0x1A),
		byte(0x04), byte(0xD3), byte(0x10), byte(0xD4), byte(0xC8), byte(0x38), byte(0xD8), byte(0xD1),
		byte(0x01), byte(0x69), byte(0x48), byte(0x2C), byte(0xCC), byte(0x44), byte(0x3D), byte(0x40),
		byte(0x4B), byte(0x20), byte(0x20), byte(0x0D), byte(0xC8), byte(0x40), byte(0x94), byte(0x44),
		byte(0x84), byte(0xD8), byte(0xC8), byte(0x23), byte(0x91), byte(0x13), byte(0x31), byte(0x12),
		byte(0x4F), byte(0x24), byte(0xCE), byte(0x08), byte(0xAB), byte(0xCE), byte(0x48), byte(0x84),
		byte(0xC8), byte(0x54), byte(0x48), byte(0x80), byte(0x51), byte(0x21), byte(0x22), byte(0x10),
		byte(0xD4), byte(0xD4), byte(0x45), byte(0x8D), byte(0x88), byte(0x34), byte(0x33), byte(0x96),
		byte(0xB0), byte(0x43), byte(0x0E), byte(0x45), byte(0x89), byte(0x17), byte(0x21), byte(0x24),
		byte(0xEB), byte(0x21), byte(0x24), byte(0xC4), byte(0x37), byte(0x24), byte(0xD1), byte(0x00),
		byte(0x81), byte(0x87), byte(0x4E), byte(0x25), byte(0x0B), byte(0x4D), byte(0x44), byte(0x44),
		byte(0x84), byte(0x82), byte(0xCB), byte(0x20), byte(0xE3), byte(0x65), byte(0x39), byte(0x13),
		byte(0x04), byte(0x46), byte(0x31), byte(0x02), byte(0x21), byte(0x22), byte(0x0E), byte(0x36),
		byte(0x43), byte(0x44), byte(0x44), byte(0x66), byte(0x2C), byte(0x39), byte(0x51), byte(0x32),
		byte(0x50), byte(0xC3), byte(0x04), byte(0x47), byte(0x63), byte(0x8D), byte(0x0C), byte(0x44),
		byte(0x71), byte(0x10), byte(0xB0), byte(0x13), byte(0x12), byte(0x05), byte(0x40), byte(0x20),
		byte(0xB0), byte(0x01), byte(0x2C), byte(0x4A), byte(0xC8), byte(0x34), byte(0x4A), byte(0xC8),
		byte(0x28), byte(0x42), byte(0xD8), byte(0xB9), byte(0x44), byte(0xD2), byte(0x20), byte(0x31),
		byte(0x32), byte(0x1C), byte(0xE4), byte(0xF2), byte(0x1C), byte(0xE4), byte(0x53), byte(0x88),
		byte(0xE5), byte(0x0D), byte(0x4D), byte(0x16), byte(0x31), byte(0x38), byte(0xB1), byte(0x20),
		byte(0x44), byte(0x40), byte(0x32), byte(0x20), byte(0xD1), byte(0x8B), byte(0x13), byte(0x15),
		byte(0x0B), byte(0x12), byte(0x30), byte(0x14), byte(0x18), byte(0x74), byte(0xC4), byte(0x46),
		byte(0xC0), byte(0x11), byte(0x28), byte(0x44), byte(0xE8), byte(0x34), byte(0x32), byte(0x02),
		byte(0x01), byte(0x31), byte(0x2F), byte(0x44), byte(0x44), byte(0x84), byte(0x35), byte(0x3A),
		byte(0xC0), byte(0x34), byte(0x38), byte(0x80), byte(0x30), byte(0xF0), byte(0x08), byte(0x18),
		byte(0xDB), byte(0x00), byte(0x4C), byte(0x44), byte(0x48), byte(0x00), byte(0xBB), byte(0xCE),
		byte(0x3D), byte(0x42), byte(0xC0), byte(0x4C), byte(0x83), byte(0x8D), byte(0x90), byte(0x23),
		byte(0x8D), byte(0x38), byte(0xC6), byte(0x2C), byte(0x10), byte(0x32), byte(0x02), byte(0x00),
		byte(0xB9), byte(0xCE), byte(0x48), byte(0xF2), byte(0x13), byte(0x00), byte(0xB8), byte(0x87),
		byte(0x51), byte(0x10), byte(0x87), byte(0x99), byte(0x13), byte(0x94), byte(0x34), byte(0x3C),
		byte(0xC7), byte(0x39), byte(0x44), byte(0x80), byte(0x34), byte(0x38), byte(0x14), byte(0x4C),
		byte(0x73), byte(0x91), byte(0x21), byte(0x36), byte(0x28), byte(0x35), byte(0x24), byte(0xC4),
		byte(0x00), byte(0x3C), byte(0x44), byte(0x08), byte(0x43), byte(0x53), byte(0x2D), byte(0x89),
		byte(0x54), byte(0x4D), byte(0x44), byte(0x44), byte(0xD9), byte(0x13), byte(0x8D), byte(0x1A),
		byte(0x83), byte(0x55), byte(0x38), byte(0xB5), byte(0x44), byte(0xAC), byte(0x81), byte(0x44),
		byte(0x9C), byte(0x42), byte(0x06), byte(0x1D), byte(0x3A), byte(0x0D), byte(0x09), byte(0x11),
		byte(0x00), byte(0x48), byte(0x4C), byte(0x48), byte(0x18), byte(0x74), byte(0xE1), byte(0x00),
		byte(0xD2), byte(0xA2), byte(0x50), byte(0xB4), byte(0xD4), byte(0x44), byte(0x02), byte(0xE2),
		byte(0x11), byte(0x14), byte(0xC0), byte(0x20), byte(0xD2), byte(0xD8), byte(0xD8), byte(0x44),
		byte(0x93), byte(0x91), byte(0x71), byte(0x02), byte(0x51), byte(0x32), byte(0x15), byte(0x12),
		byte(0x13), byte(0x80), byte(0x44), byte(0x3C), byte(0x84), byte(0x10), byte(0xAA), byte(0xCE),
		byte(0x34), byte(0x6B), byte(0x85), byte(0x14), byte(0x80), byte(0x84), byte(0x47), byte(0x24),
		byte(0xC0), byte(0x4C), byte(0x43), byte(0x04), byte(0x35), byte(0x3C), byte(0x44), byte(0x49),
		byte(0x38), byte(0x40), byte(0x62), byte(0x31), byte(0x00), byte(0x2F), byte(0x63), byte(0x91),
		byte(0x28), byte(0x44), byte(0x71), byte(0x11), byte(0x23), byte(0x94), byte(0x44), byte(0x21),
		byte(0x33), byte(0x1D), byte(0x13), byte(0x96), byte(0x94), byte(0xE4), byte(0x56), byte(0x01),
		byte(0x10), byte(0xEF), byte(0x38), byte(0xB2), byte(0x02), byte(0x63), byte(0x20), byte(0x88),
		byte(0x10), byte(0xD0), byte(0x84), byte(0x91), byte(0x81), byte(0x12), byte(0x84), byte(0x40),
		byte(0xE8), byte(0x4C), byte(0x43), byte(0x36), byte(0x10), byte(0x03), byte(0xCE), byte(0x36),
		byte(0x52), byte(0x0B), byte(0x2E), byte(0xF2), byte(0xC0), byte(0x36), byte(0xC2), byte(0x0B),
		byte(0x21), byte(0x30), byte(0x11), byte(0x62), byte(0x65), byte(0x0D), byte(0x9C), byte(0xE4),
		byte(0xE7), byte(0x10), byte(0x04), byte(0xE0), byte(0x0C), byte(0x34), byte(0x44), byte(0x49),
		byte(0x28), byte(0x8E), byte(0x2C), byte(0x39), byte(0x4E), byte(0x09), byte(0x44), byte(0xA5),
		byte(0x39), byte(0x11), byte(0x08), byte(0x18), byte(0xDC), byte(0xD1), byte(0x10), byte(0x04),
		byte(0xCC), byte(0x10), byte(0xD4), byte(0xE1), byte(0x2C), byte(0xE3), byte(0x83), byte(0xD0),
		byte(0xF3), byte(0x8D), byte(0x88), byte(0xE5), byte(0x11), byte(0x48), byte(0x4C), byte(0xC7),
		byte(0x21), byte(0x10), byte(0xF6), byte(0x01), byte(0x30), byte(0x87), byte(0x80), byte(0x51),
		byte(0x44), byte(0x09), byte(0x39), byte(0x00), byte(0x44), byte(0xB6), byte(0x32), byte(0x4C),
		byte(0xE4), byte(0x44), byte(0xCC), byte(0x75), byte(0x12), byte(0xC8), byte(0xE5), byte(0x0D),
		byte(0x0E), byte(0x45), byte(0x44), byte(0x45), byte(0x85), byte(0x87), byte(0x11), byte(0x11),
		byte(0x21), byte(0x00), byte(0x16), byte(0x20), byte(0x0C), byte(0xC2), byte(0x0D), byte(0x21),
		byte(0x24), byte(0xD1), byte(0x01), byte(0x32), byte(0x0E), byte(0x36), byte(0xC3), byte(0x94),
		byte(0x4C), byte(0x7B), byte(0xC0), byte(0x18), byte(0x49), byte(0x0D), byte(0x4C), byte(0x44),
		byte(0x6F), byte(0x44), byte(0xE0), byte(0x40), byte(0x04), byte(0xB6), byte(0x2F), byte(0x38),
		byte(0x83), byte(0x53), byte(0xC8), byte(0x40), byte(0x13), byte(0xB4), byte(0x04), byte(0xD4),
		byte(0x44), byte(0x02), byte(0xF1), byte(0x00), byte(0x21), byte(0x25), byte(0x01), byte(0x18),
		byte(0x87), byte(0x00), byte(0xB2), byte(0xC4), byte(0x34), byte(0x61), byte(0x2F), byte(0x01),
		byte(0x24), byte(0xA0), byte(0x3C), byte(0xF2), byte(0xD8), byte(0xB0), byte(0x02), byte(0x0B),
		byte(0xD1), byte(0x25), byte(0x00), byte(0x2C), byte(0xB6), byte(0x2C), byte(0x21), byte(0x7C),
		byte(0xCE), byte(0x50), byte(0x61), byte(0xE2), byte(0x2C), byte(0x40), byte(0x11), byte(0x2D),
		byte(0x89), byte(0x91), byte(0x39), byte(0x69), byte(0x40), byte(0x09), byte(0x33), byte(0x91),
		byte(0xC9), byte(0x30), byte(0x13), byte(0x12), byte(0xB3), byte(0x82), byte(0x00), byte(0xB9),
		byte(0x94), byte(0x62), byte(0x40), byte(0x12), byte(0x4F), byte(0x20), byte(0x15), byte(0x13),
		byte(0x23), byte(0x94), byte(0x4C), byte(0x7C), byte(0x82), byte(0x10), byte(0xD1), byte(0x2C),
		byte(0x39), byte(0x31), byte(0xC4), byte(0x46), byte(0x20), byte(0x11), byte(0x10), byte(0x44),
		byte(0x70), byte(0x50), byte(0x80), byte(0x8A), byte(0x2D), byte(0x88), byte(0x84), byte(0x35),
		byte(0x34), byte(0x40), byte(0x2E), byte(0x50), byte(0x02), byte(0x12), byte(0x80), byte(0x84),
		byte(0x80), byte(0x13), byte(0x95), byte(0x12), byte(0x11), byte(0x18), byte(0x38), byte(0xD0),
		byte(0xEF), byte(0x20), byte(0x24), byte(0xD4), byte(0x44), byte(0x4B), byte(0x44), byte(0x4D),
		byte(0x63), byte(0x91), byte(0x2A), byte(0xC0), byte(0x0D), byte(0x00), byte(0x61), byte(0x0C),
		byte(0x10), byte(0xD4), byte(0xE8), byte(0x34), byte(0x32), byte(0x15), byte(0x20), byte(0x35),
		byte(0x00), byte(0x2E), byte(0x50), byte(0x0D), byte(0xC8), byte(0x86), byte(0x44), byte(0xC8),
		byte(0xF1), byte(0x04), byte(0x0E), byte(0x15), byte(0x12), byte(0x63), byte(0x21), byte(0x11),
		byte(0x20), byte(0xE5), byte(0x12), byte(0xB8), byte(0x20), byte(0x94), byte(0x46), byte(0x00),
		byte(0xC3), byte(0xC4), byte(0x40), byte(0x03), byte(0x63), byte(0x22), byte(0x06), byte(0x36),
		byte(0x23), byte(0x8B), byte(0x2C), byte(0x40), byte(0x93), byte(0x20), byte(0xE3), byte(0x6B),
		byte(0x21), byte(0x24), byte(0xE0), byte(0x3C), byte(0xF4), byte(0x4E), byte(0x00), byte(0x21),
		byte(0xE2), byte(0x1C), byte(0x04), byte(0x46), byte(0x13), byte(0x05), byte(0x00), byte(0x2C),
		byte(0x84), byte(0xD8), byte(0xBD), byte(0x11), byte(0x12), byte(0x49), byte(0x44), byte(0x44),
		byte(0xD4), byte(0xE4), byte(0xC4), byte(0xB4), byte(0xE4), byte(0xC4), byte(0xBC), byte(0x04),
		byte(0x53), byte(0xC4), byte(0x40), byte(0x0B), byte(0xD8), byte(0x40), byte(0x62), byte(0x51),
		byte(0x14), byte(0x44), byte(0x35), byte(0x38), byte(0xC4), byte(0x4C), byte(0x44), byte(0x4C),
		byte(0x20), byte(0xD1), byte(0x33), byte(0x45), byte(0x41), byte(0x32), byte(0x00), byte(0x3D),
		byte(0x87), byte(0x01), byte(0x31), byte(0x15), byte(0x11), byte(0x18), byte(0x51), byte(0x10),
		byte(0x02), byte(0xB6), byte(0x39), byte(0x14), byte(0x58), byte(0x89), byte(0x43), byte(0xEF),
		byte(0x01), byte(0x14), byte(0xC8), byte(0x09), byte(0x42), byte(0xC0), byte(0x44), byte(0xB6),
		byte(0x20), byte(0x30), byte(0xE5), byte(0x0D), byte(0x4E), byte(0x00), byte(0x48), byte(0x2C),
		byte(0x84), byte(0xD8), byte(0x90), byte(0x04), byte(0xF1), byte(0x10), byte(0x23), byte(0x86),
		byte(0x34), byte(0x86), byte(0x44), byte(0xC8), byte(0x84), byte(0xE2), byte(0x1C), byte(0x04),
		byte(0x40), byte(0x09), byte(0x31), byte(0x11), byte(0xC8), byte(0xE3), byte(0x04), byte(0x04),
		byte(0xE0), byte(0xD8), byte(0xAC), byte(0xE4), byte(0x92), byte(0x8C), byte(0x41), byte(0x91),
		byte(0x10), byte(0x49), byte(0x05), byte(0x14), byte(0x40), byte(0x93), byte(0x81), byte(0x34),
		byte(0xC0), byte(0x08), byte(0xAC), byte(0x93), byte(0x00), byte(0x51), byte(0x6C), byte(0x20),
		byte(0x30), byte(0xCB), byte(0x13), byte(0x31), byte(0x0B), byte(0x11), byte(0x52), byte(0x12),
		byte(0x20), byte(0xE3), byte(0x76), byte(0x1D), byte(0x8A), byte(0xC4), byte(0x18), byte(0x02),
		byte(0xE2), byte(0x00), byte(0xF2), byte(0x13), byte(0x00), byte(0xBC), byte(0xD1), byte(0x00),
		byte(0x31), byte(0x24), byte(0x2C), byte(0x40), byte(0x93), byte(0x20), byte(0xE3), byte(0x64),
		byte(0x54), byte(0x44), byte(0x58), byte(0x04), byte(0xE0), byte(0xD8), byte(0x8D), byte(0x13),
		byte(0x8F), byte(0xB0), byte(0x02), byte(0x4E), byte(0x47), byte(0x52), byte(0x04), byte(0x5B),
		byte(0x24), byte(0xC0), byte(0x34), byte(0x30), byte(0x11), byte(0x0E), byte(0x12), byte(0x0B),
		byte(0x2E), byte(0x43), byte(0x0F), byte(0x2C), byte(0xE6), byte(0x04), byte(0x12), byte(0x32),
		byte(0x12), byte(0x09), byte(0x44), byte(0x92), byte(0x20), byte(0xE3), byte(0x6E), byte(0x3C),
		byte(0xF3), byte(0x91), byte(0x4D), byte(0x43), byte(0x48), byte(0x4D), byte(0x88), byte(0x0D),
		byte(0x00), byte(0xB6), byte(0x12), byte(0x21), byte(0x2C), byte(0xC4), byte(0x37), byte(0x25),
		byte(0x06), byte(0x18), byte(0x44), byte(0x93), byte(0xAC), byte(0x05), byte(0x98), byte(0x11),
		byte(0x19), byte(0xD4), byte(0x48), byte(0x10), byte(0x0D), byte(0x0F), byte(0x21), byte(0x02),
		byte(0x4C), byte(0x83), byte(0x8D), byte(0x84), byte(0x40), byte(0x8E), byte(0x30), byte(0x4C),
		byte(0x8A), byte(0x20), byte(0xB2), byte(0xF2), byte(0x21), byte(0x24), byte(0xC4), byte(0x47),
		byte(0x24), byte(0xD8), byte(0x2C), byte(0x48), byte(0x91), byte(0x20), byte(0xC1), byte(0x2F),
		byte(0x44), byte(0xE1), byte(0x91), byte(0x00), byte(0xC8), byte(0x8E), byte(0x30), byte(0xF0),
		byte(0x11), byte(0x12), byte(0x20), byte(0x0F), byte(0xB0), byte(0x84), byte(0x92), byte(0x84),
		byte(0x00), byte(0xF2), byte(0x39), byte(0x14), byte(0xF3), byte(0x44), byte(0x02), byte(0x0D),
		byte(0x20), byte(0xD1), byte(0xA4), byte(0x01), byte(0x26), byte(0x2D), byte(0x10), byte(0x04),
		byte(0x71), byte(0x10), byte(0x62), byte(0x0E), byte(0x37), byte(0x24), byte(0xD1), byte(0x01),
		byte(0x31), byte(0x06), byte(0x62), byte(0xF5), byte(0x11), byte(0x3C), byte(0xE4), byte(0x84),
		byte(0xBC), byte(0x44), byte(0x45), byte(0x39), byte(0x13), byte(0x33), byte(0x10), byte(0x21),
		byte(0xCD), byte(0x38), byte(0xB3), byte(0x86), byte(0x62), byte(0x40), byte(0x8E), byte(0x34),
		byte(0xE3), byte(0x08), byte(0x0A), byte(0x15), byte(0x03), byte(0x18), byte(0x44), byte(0xE4),
		byte(0x5C), byte(0x03), byte(0x0F), byte(0x2C), byte(0x48), byte(0x87), byte(0x10), byte(0x22),
		byte(0xA4), byte(0x35), byte(0x52), byte(0x11), byte(0x38), byte(0xD3), byte(0x04), byte(0x35),
		byte(0x3A), byte(0xC4), byte(0x1A), byte(0x30), byte(0x11), byte(0x2B), byte(0x31), byte(0x11),
		byte(0x33), byte(0x10), byte(0x13), byte(0x1C), byte(0x44), byte(0x6B), byte(0x01), byte(0x41),
		byte(0x87), byte(0x99), byte(0x41), byte(0x12), byte(0x4A), byte(0x20), byte(0x11), byte(0xAC),
		byte(0xE5), byte(0x84), byte(0x46), byte(0x70), byte(0x0D), byte(0x1A), byte(0xF0), byte(0x12),
		byte(0x4F), byte(0x23), byte(0x82), byte(0x20), byte(0x02), byte(0xE5), byte(0x39), byte(0x11),
		byte(0x84), byte(0x4E), byte(0x75), byte(0x0D), byte(0x0D), byte(0x11), byte(0x03), byte(0xC4),
		byte(0x43), byte(0x0E), byte(0x54), byte(0x4B), byte(0x00), byte(0x34), byte(0x01), byte(0x84),
		byte(0x46), byte(0x43), byte(0x49), byte(0x39), byte(0x89), byte(0x17), byte(0x00), byte(0x24),
		byte(0xCB), byte(0x62), byte(0x32), byte(0x04), byte(0x94), byte(0x83), byte(0x40), byte(0x2E),
		byte(0xC0), byte(0x18), byte(0x04), byte(0x49), byte(0xC4), byte(0x00), byte(0xB4), byte(0xC7),
		byte(0x94), byte(0xB3), byte(0x8E), byte(0x46), byte(0x21), byte(0xC0), byte(0x34), byte(0x61),
		byte(0x2B), byte(0x01), byte(0x8B), byte(0xCE), byte(0x39), byte(0x19), byte(0x54), byte(0x36),
		byte(0x44), byte(0x93), byte(0x00), byte(0x12), byte(0xC8), byte(0x48), byte(0x7C), byte(0xD1),
		byte(0x20), byte(0x02), byte(0xF2), byte(0x3D), byte(0x12), byte(0x0D), byte(0x1A), byte(0x32),
		byte(0x0D), byte(0x34), byte(0x44), byte(0x61), byte(0x20), byte(0x6C), byte(0xC7), byte(0x00),
		byte(0xD2), byte(0xAF), byte(0x44), byte(0xE4), byte(0xC4), byte(0x09), byte(0x38), byte(0x15),
		byte(0x38), byte(0x80), byte(0xE8), byte(0x30), byte(0x01), byte(0x88), byte(0x34), byte(0x4C),
		byte(0xCE), byte(0x34), byte(0x81), byte(0x87), byte(0x4F), byte(0x24), byte(0xC0), byte(0x46),
		byte(0x04), byte(0x4C), byte(0x94), byte(0x83), byte(0x48), byte(0x48), byte(0x7B), byte(0x14),
		byte(0x48), byte(0x80), byte(0xAE), byte(0x58), byte(0xD1), byte(0x11), byte(0x89), byte(0x16),
		byte(0x20), byte(0x45), byte(0x3B), byte(0xD1), byte(0x21), byte(0x50), byte(0x13), byte(0x12),
		byte(0xE4), byte(0xC7), byte(0x11), byte(0x14), byte(0xB2), byte(0x20), byte(0xC3), byte(0xCB),
		byte(0x12), byte(0xF3), byte(0x8F), byte(0x50), byte(0xB0), byte(0x11), byte(0xC4), byte(0x41),
		byte(0x4B), byte(0x10), byte(0x24), byte(0xE4), byte(0x48), byte(0xF1), byte(0x02), byte(0x20),
		byte(0x02), byte(0xCB), byte(0x63), byte(0x23), byte(0x00), byte(0x2C), byte(0xBA), byte(0xC8),
		byte(0x18), byte(0x74), byte(0xEC), byte(0x11), byte(0x24), byte(0x80), byte(0x18), byte(0x4C),
		byte(0x93), byte(0x10), byte(0xFA), byte(0x84), byte(0x62), byte(0xF1), byte(0x00), byte(0x08),
		byte(0x4B), byte(0xD1), byte(0x38), byte(0x64), byte(0x44), byte(0x49), byte(0x28), byte(0x40),
		byte(0x37), byte(0x22), byte(0x03), byte(0x12), byte(0x64), byte(0x44), byte(0x01), byte(0x39),
		byte(0x48), byte(0x5E), byte(0x83), byte(0x53), byte(0x11), byte(0x15), byte(0x48), byte(0x11),
		byte(0x6B), byte(0x00), byte(0x34), byte(0x01), byte(0x84), byte(0xB4), byte(0x04), byte(0xC8),
		byte(0x38), byte(0xD0), byte(0x0B), byte(0x94), byte(0x84), byte(0x87), byte(0xAC), byte(0xE4),
		byte(0x84), byte(0x88), byte(0x03), byte(0x04), byte(0x44), byte(0x08), byte(0xC8), byte(0x48),
		byte(0x25), byte(0x12), byte(0x4A), byte(0x44), byte(0x14), byte(0x00), byte(0xBD), byte(0x84),
		byte(0x20), byte(0x61), byte(0xD3), byte(0xBC), byte(0x44), byte(0x45), byte(0x39), byte(0x13),
		byte(0x00), byte(0x34), byte(0x21), byte(0x32), byte(0x11), byte(0x51), byte(0x0D), byte(0xD8),
		byte(0x04), byte(0xC4), byte(0x46), byte(0xF4), byte(0x4E), byte(0x0D), byte(0x40), byte(0x93),
		byte(0x20), byte(0xE3), byte(0x6F), byte(0x11), byte(0x14), byte(0x8E), byte(0x34), byte(0x02),
		byte(0xE2), byte(0x10), byte(0xB2), byte(0xEF), byte(0x39), byte(0x61), byte(0x11), byte(0x91),
		byte(0x51), byte(0x0D), byte(0x20), byte(0xD1), byte(0xA2), byte(0x38), byte(0xB3), byte(0x91),
		byte(0xA0), byte(0xD4), byte(0x88), byte(0x0C), byte(0x48), byte(0x40), byte(0x47), byte(0x43),
		byte(0x48), byte(0x4E), byte(0xB1), byte(0x12), byte(0x4A), byte(0x00), byte(0xD4), byte(0x2D),
		byte(0x3D), byte(0x88), byte(0x0C), byte(0x4C), byte(0x40), byte(0x34), byte(0x61), byte(0x2C),
		byte(0x10), byte(0xD4), byte(0xC8), byte(0x38), byte(0xD8), byte(0xC4), byte(0x10), byte(0xF9),
		byte(0x03), byte(0x18), byte(0x4C), byte(0x93), byte(0x44), byte(0xE3), byte(0x46), byte(0x9C),
		byte(0x04), byte(0x43), byte(0xCD), byte(0x13), byte(0x94), byte(0x04), byte(0xB1), byte(0x2D),
		byte(0x10), byte(0x21), byte(0x12), byte(0x48), byte(0x04), byte(0x58), byte(0xC8), byte(0x01),
		byte(0x44), byte(0x88), byte(0xE3), byte(0x0C), byte(0x38), byte(0xD9), byte(0x44), byte(0x01),
		byte(0x19), byte(0x40), byte(0x30), byte(0x82), byte(0xD8), byte(0xC8), byte(0x40), byte(0x23),
		byte(0x44), byte(0x40), byte(0x0C), byte(0x88), byte(0xE3), byte(0x45), byte(0x11), byte(0x11),
		byte(0x0D), byte(0x08), byte(0x4C), byte(0x44), byte(0x3C), byte(0xB6), byte(0x2F), byte(0x44),
		byte(0xE3), byte(0xC4), byte(0x45), byte(0x36), byte(0x2C), byte(0x10), byte(0x44), byte(0xC8),
		byte(0x34), byte(0x68), byte(0x0B), byte(0x58), byte(0x06), byte(0x12), byte(0xC9), byte(0x35),
		byte(0x05), byte(0x16), byte(0x01), byte(0x84), byte(0x34), byte(0x26), byte(0x23), byte(0x10),
		byte(0x04), byte(0xC7), byte(0x99), byte(0x13), byte(0x96), byte(0x4C), byte(0x7C), byte(0x84),
		byte(0x2C), byte(0xBC), byte(0x8E), byte(0x2C), byte(0x32), byte(0x04), byte(0x46), byte(0x00),
		byte(0x93), byte(0x9C), byte(0x40), byte(0x15), byte(0x63), byte(0x61), byte(0x13), byte(0x84),
		byte(0x01), byte(0xAC), byte(0x01), byte(0x14), byte(0x48), byte(0x00), byte(0x61), byte(0x23),
		byte(0x10), byte(0x00), byte(0xF2), byte(0x20), byte(0xD1), byte(0xB1), byte(0x21), byte(0x21),
		byte(0x23), byte(0x10), byte(0x20), byte(0x03), byte(0x13), byte(0x61), byte(0xCE), byte(0x32),
		byte(0x52), byte(0x06), byte(0x51), byte(0x11), byte(0x2F), byte(0x38), byte(0xB2), byte(0x02),
		byte(0x12), byte(0x13), byte(0x83), byte(0x62), byte(0xC0), byte(0x02), byte(0x1C), byte(0x83),
		byte(0x44), byte(0x88), byte(0x04), byte(0xC4), byte(0x18), byte(0xE4), byte(0x58), byte(0x80),
		byte(0x71), byte(0x00), byte(0x0E), byte(0x54), byte(0x4E), byte(0x35), byte(0x38), byte(0x80),
		byte(0x44), byte(0x4B), byte(0x91), byte(0x0C), byte(0x44), byte(0x71), byte(0x10), byte(0x02),
		byte(0xC8), byte(0x4D), byte(0x8B), byte(0xC0), byte(0x45), byte(0x33), byte(0x44), byte(0x47),
		byte(0x80), byte(0x11), byte(0x0E), byte(0x11), byte(0x00), byte(0x4F), byte(0x52), byte(0x0E),
		byte(0x2C), byte(0x43), byte(0x42), byte(0x13), byte(0x33), byte(0x93), byte(0x00), byte(0xB8),
		byte(0xC4), byte(0x14), byte(0x43), byte(0x52), byte(0x13), byte(0x64), byte(0x48), byte(0x4C),
		byte(0x48), byte(0x8E), byte(0x35), byte(0x25), byte(0x0C), byte(0x11), byte(0x18), byte(0x84),
		byte(0x35), byte(0x31), byte(0x11), byte(0x99), byte(0x13), byte(0x94), byte(0x3F), byte(0x31),
		byte(0xCE), byte(0x50), byte(0x61), byte(0xD3), byte(0xB0), byte(0xE0), byte(0xC4), byte(0x44),
		byte(0xDC), byte(0xC0), byte(0x48), byte(0xA8), byte(0x8E), byte(0x00), byte(0x21), byte(0xF1),
		byte(0x10), byte(0x04), byte(0x8E), byte(0x36), byte(0x01), byte(0x84), byte(0x94), byte(0x83),
		byte(0x46), byte(0x11), byte(0x1C), byte(0x8F), byte(0x10), byte(0x22), byte(0x05), byte(0x20),
		byte(0x28), byte(0x8E), byte(0x34), byte(0xD1), byte(0x02), byte(0x4C), byte(0x83), byte(0x8D),
		byte(0xD8), byte(0x84), byte(0x87), byte(0xC4), byte(0x44), byte(0x8F), byte(0x38), byte(0xD4),
		byte(0x84), byte(0xBD), byte(0x11), byte(0x13), byte(0x4D), byte(0x8B), byte(0x0E), byte(0x54),
		byte(0x43), byte(0x04), byte(0x35), byte(0x38), byte(0x80), byte(0x44), byte(0x3A), byte(0xCE),
		byte(0x1A), byte(0xF1), byte(0x0D), byte(0xC9), byte(0x43), byte(0x33), byte(0x44), byte(0x41),
		byte(0x24), byte(0x35), byte(0x32), byte(0x11), byte(0x12), byte(0x22), byte(0x13), byte(0x21),
		byte(0x91), byte(0x0D), byte(0xCC), byte(0x74), byte(0x4E), byte(0x50), byte(0x61), byte(0xCE),
		byte(0x51), byte(0x3B), byte(0xC4), byte(0x4F), byte(0x22), byte(0x0C), byte(0x20), byte(0xB0),
		byte(0x11), byte(0xD4), byte(0x80), byte(0x93), byte(0x20), byte(0xCB), byte(0x44), byte(0x59),
		byte(0x23), byte(0xC0), byte(0x3C), byte(0x44), byte(0x73), byte(0x1D), byte(0x11), byte(0x00),
		byte(0x4E), byte(0x22), byte(0xC0), byte(0x49), byte(0x2C), byte(0x87), byte(0x00), byte(0xA1),
		byte(0x32), byte(0x39), byte(0x44), byte(0x42), byte(0x12), byte(0x00), byte(0x82), byte(0x39),
		byte(0x43), byte(0x53), byte(0xBC), byte(0x02), byte(0x0D), byte(0x94), byte(0x02), byte(0xCB),
		byte(0xC4), byte(0x80), byte(0x87), byte(0xBC), byte(0xE4), byte(0x92), byte(0x20), byte(0x12),
		byte(0xC4), byte(0x80), byte(0x20), byte(0x84), byte(0x3D), byte(0x3C), byte(0x8E), byte(0x2C),
		byte(0x80), byte(0xF3), byte(0x44), byte(0x05), byte(0x44), byte(0x2F), byte(0x30), byte(0x0B),
		byte(0x2B), byte(0x22), byte(0x98), byte(0x89), byte(0x11), byte(0x00), byte(0x4C), byte(0x4B),
		byte(0x4E), byte(0x34), byte(0x4B), byte(0xCB), byte(0x10), byte(0xD4), byte(0xD8), byte(0xBC),
		byte(0x44), byte(0x48), byte(0x38), byte(0x38), byte(0xC4), byte(0x14), byte(0x83), byte(0x44),
		byte(0xB4), byte(0xE4), byte(0x4C), byte(0x00), byte(0xBC), byte(0x44), byte(0x54), byte(0x40),
		byte(0x0B), byte(0x8D), byte(0x12), byte(0x0D), byte(0x2A), byte(0x05), byte(0x13), byte(0x1C),
		byte(0xE4), byte(0x72), byte(0x11), byte(0x15), byte(0x44), byte(0xB4), byte(0x03), byte(0x04),
		byte(0xB0), byte(0xE3), byte(0x04), byte(0x35), byte(0x38), byte(0x06), byte(0x10), byte(0xD4),
		byte(0xE3), byte(0x38), byte(0x25), byte(0x0C), byte(0x10), byte(0xD4), byte(0xE0), byte(0x09),
		byte(0x32), byte(0x15), byte(0x21), byte(0x36), byte(0x20), byte(0x35), byte(0x85), byte(0x80),
		byte(0x62), byte(0x01), byte(0x51), byte(0x00), byte(0x80), byte(0xF3), byte(0x60), byte(0xF1),
		byte(0x20), byte(0x09), byte(0x32), byte(0x15), byte(0x13), byte(0x34), byte(0x40), byte(0x20),
		byte(0xDA), byte(0x0D), byte(0x4C), byte(0x44), byte(0x44), byte(0x49), byte(0x32), byte(0x0D),
		byte(0x1B), byte(0x10), byte(0x03), byte(0x20), byte(0xE8), byte(0xC0), byte(0x34), byte(0x61),
		byte(0x11), byte(0x98), byte(0x43), byte(0x44), byte(0x44), byte(0x04), byte(0xC8), byte(0x38),
		byte(0xDA), byte(0xC4), byte(0x00), byte(0x58), byte(0x8E), byte(0x3D), byte(0x8B), byte(0x00),
		byte(0x4C), byte(0x21), byte(0xE2), byte(0x2C), byte(0x02), byte(0x0C), byte(0x80), byte(0xD6),
		byte(0x0E), byte(0x34), byte(0x4C), byte(0x8E), byte(0x15), byte(0x35), byte(0x80), byte(0x44),
		byte(0x4B), byte(0xC0), byte(0x45), byte(0x36), byte(0x23), byte(0x11), byte(0x52), byte(0x02),
		byte(0x12), byte(0x23), byte(0x83), byte(0x12), byte(0xB0), byte(0x0D), byte(0x19), byte(0x40),
		byte(0x06), byte(0x12), byte(0xB2), byte(0x0D), byte(0x2A), byte(0x73), byte(0x96), byte(0x11),
		byte(0x51), byte(0x11), byte(0x88), byte(0xE3), byte(0x45), byte(0x21), byte(0x13), byte(0x22),
		byte(0x38), byte(0xC3), byte(0x04), byte(0x35), byte(0x38), byte(0x88), byte(0x4D), byte(0x88),
		byte(0x0D), byte(0x61), byte(0x61), byte(0xC4), byte(0x44), byte(0x4C), byte(0x8E), byte(0x30),
		byte(0x45), byte(0x87), byte(0x11), byte(0x11), byte(0x23), byte(0x10), byte(0x10), byte(0x13),
		byte(0x12), byte(0x34), byte(0x48), byte(0x54), byte(0x49), byte(0xC8), byte(0x18), byte(0x71),
		byte(0x11), byte(0x84), byte(0x40), byte(0x14), byte(0x4C), byte(0x81), byte(0x54), byte(0x2E),
		byte(0xE3), byte(0x4B), byte(0x20), byte(0xD1), byte(0x36), byte(0x38), byte(0xC0), byte(0x0D),
		byte(0xBD), byte(0x12), byte(0x0E), byte(0x44), byte(0x84), byte(0xD8), byte(0xCD), byte(0x10),
		byte(0x03), byte(0x21), byte(0x32), byte(0x0E), byte(0x34), byte(0x02), byte(0xE5), byte(0x39),
		byte(0x44), byte(0x65), byte(0x20), byte(0xD0), byte(0x0D), byte(0x08), byte(0x80), byte(0x0B),
		byte(0x79), byte(0xE7), byte(0x9E),
	}
)

// return 8-bit status (see MASK flags constants)
func computeStats(block []byte, freqs0 []int32) byte {
	var freqs [256][256]int32
	freqs1 := freqs[0:256]
	length := len(block)
	end4 := length & -4
	prv := int32(0)

	// Unroll loop
	for i := 0; i < end4; i += 4 {
		cur0 := int32(block[i])
		cur1 := int32(block[i+1])
		cur2 := int32(block[i+2])
		cur3 := int32(block[i+3])
		freqs0[cur0]++
		freqs0[cur1]++
		freqs0[cur2]++
		freqs0[cur3]++
		freqs1[prv][cur0]++
		freqs1[cur0][cur1]++
		freqs1[cur1][cur2]++
		freqs1[cur2][cur3]++
		prv = cur3
	}

	for i := end4; i < length; i++ {
		cur := int32(block[i])
		freqs0[cur]++
		freqs1[prv][cur]++
		prv = cur
	}

	nbTextChars := 0

	for i := 32; i < 128; i++ {
		if isText(byte(i)) {
			nbTextChars += int(freqs0[i])
		}
	}

	// Not text (crude threshold)
	if 2*nbTextChars < length {
		return TC_MASK_NOT_TEXT
	}

	nbBinChars := 0

	for i := 128; i < 256; i++ {
		nbBinChars += int(freqs0[i])
	}

	// Not text (crude threshold)
	if 4*nbBinChars > length {
		return TC_MASK_NOT_TEXT
	}

	res := byte(0)

	if nbBinChars == 0 {
		res |= TC_MASK_FULL_ASCII
	} else if nbBinChars <= length/100 {
		res |= TC_MASK_ALMOST_FULL_ASCII
	}

	if nbBinChars <= length-length/10 {
		// Check if likely XML/HTML
		// Another crude test: check that the frequencies of < and > are similar
		// and 'high enough'. Also check it is worth to attempt replacing ampersand sequences.
		// Getting this flag wrong results in a very small compression speed degradation.
		f1 := freqs0['<']
		f2 := freqs0['>']
		f3 := freqs['&']['a'] + freqs['&']['g'] + freqs['&']['l'] + freqs['&']['q']
		minFreq := int32(length-nbBinChars) >> 9

		if minFreq < 2 {
			minFreq = 2
		}

		if (f1 >= minFreq) && (f2 >= minFreq) && (f3 > 0) {
			if f1 < f2 {
				if f1 >= f2-f2/100 {
					res |= TC_MASK_XML_HTML
				}
			} else if f2 < f1 {
				if f2 >= f1-f1/100 {
					res |= TC_MASK_XML_HTML
				}
			} else {
				res |= TC_MASK_XML_HTML
			}
		}
	}

	// Check CR+LF matches
	if (freqs0[CR] != 0) && (freqs0[CR] == freqs0[LF]) {
		isCRLF := true

		for i := 0; i < 256; i++ {
			if (i != int(LF)) && (freqs1[CR][i] != 0) {
				isCRLF = false
				break
			}
		}

		if isCRLF == true {
			res |= TC_MASK_CRLF
		}
	}

	return res
}

func sameWords(buf1, buf2 []byte) bool {
	for i := range buf1 {
		if buf1[i] != buf2[i] {
			return false
		}
	}

	return true
}

func initDelimiterChars() []bool {
	var res [256]bool

	for i := range res {
		if (i >= ' ') && (i <= '/') { // [ !"#$%&'()*+,-./]
			res[i] = true
		} else if (i >= ':') && (i <= '?') { // [:;<=>?]
			res[i] = true
		} else {
			switch i {
			case '\n':
				fallthrough
			case '\r':
				fallthrough
			case '\t':
				fallthrough
			case '_':
				fallthrough
			case '|':
				fallthrough
			case '{':
				fallthrough
			case '}':
				fallthrough
			case '[':
				fallthrough
			case ']':
				res[i] = true
			default:
				res[i] = false
			}
		}
	}

	return res[:]
}

// Create dictionary from array of words
func createDictionary(words []byte, dict []dictEntry, maxWords, startWord int) int {
	anchor := 0
	h := TC_HASH1
	nbWords := startWord

	for i := 0; (i < len(words)) && (nbWords < maxWords); i++ {
		cur := words[i]

		if isText(cur) {
			h = h*TC_HASH1 ^ int32(cur)*TC_HASH2
			continue
		}

		if isDelimiter(cur) && (i >= anchor+1) { // At least 2 letters
			dict[nbWords] = dictEntry{ptr: words[anchor:i], hash: h, data: int32(((i - anchor) << 24) | nbWords)}
			nbWords++
		}

		anchor = i + 1
		h = TC_HASH1
	}

	return nbWords
}

func isText(val byte) bool {
	return isLowerCase(val) || isUpperCase(val)
}

func isLowerCase(val byte) bool {
	return (val >= 'a') && (val <= 'z')
}

func isUpperCase(val byte) bool {
	return (val >= 'A') && (val <= 'Z')
}

func isDelimiter(val byte) bool {
	return TC_DELIMITER_CHARS[val]
}

// Unpack dictionary with 32 symbols (all lowercase except first word char)
func unpackDictionary32(dict []byte) []byte {
	buf := make([]byte, len(dict)*2)
	d := 0
	val := 0

	// Unpack 3 bytes into 4 6-bit symbols
	for i := range dict {
		val = (val << 8) | int(dict[i]&0xFF)

		if (i % 3) == 2 {
			for ii := 18; ii >= 0; ii -= 6 {
				c := (val >> uint(ii)) & 0x3F

				if c >= 32 {
					buf[d] = ' '
					d++
				}

				c &= 0x1F

				// Ignore padding symbols (> 26 and <= 31)
				if c <= 26 {
					buf[d] = byte(c + 'a')
					d++
				}
			}

			val = 0
		}
	}

	buf[d] = ' ' // End
	return buf[1 : d+1]
}

func NewTextCodec() (*TextCodec, error) {
	this := new(TextCodec)
	d, err := newTextCodec1()
	this.delegate = d
	return this, err
}

func NewTextCodecWithCtx(ctx *map[string]interface{}) (*TextCodec, error) {
	this := new(TextCodec)

	var err error
	var d kanzi.ByteFunction

	if val, containsKey := (*ctx)["textcodec"]; containsKey {
		encodingType := val.(int)

		if encodingType == 2 {
			d, err = newTextCodec2WithCtx(ctx)
			this.delegate = d
		}
	}

	if this.delegate == nil && err == nil {
		d, err = newTextCodec1WithCtx(ctx)
		this.delegate = d
	}

	return this, err
}

func (this *TextCodec) Forward(src, dst []byte) (uint, uint, error) {
	if len(src) == 0 {
		return 0, 0, nil
	}

	if &src[0] == &dst[0] {
		return 0, 0, errors.New("Input and output buffers cannot be equal")
	}

	if len(src) > TC_MAX_BLOCK_SIZE {
		// Not a recoverable error: instead of silently fail the transform,
		// issue a fatal error.
		errMsg := fmt.Sprintf("The max text transform block size is %v, got %v", TC_MAX_BLOCK_SIZE, len(src))
		panic(errors.New(errMsg))
	}

	return this.delegate.Forward(src, dst)
}

func (this *TextCodec) Inverse(src, dst []byte) (uint, uint, error) {
	if len(src) == 0 {
		return 0, 0, nil
	}

	if &src[0] == &dst[0] {
		return 0, 0, errors.New("Input and output buffers cannot be equal")
	}

	if len(src) > TC_MAX_BLOCK_SIZE {
		// Not a recoverable error: instead of silently fail the transform,
		// issue a fatal error.
		errMsg := fmt.Sprintf("The max text transform block size is %v, got %v", TC_MAX_BLOCK_SIZE, len(src))
		panic(errors.New(errMsg))
	}

	return this.delegate.Inverse(src, dst)

}

func (this *TextCodec) MaxEncodedLen(srcLen int) int {
	return this.delegate.MaxEncodedLen(srcLen)
}

func newTextCodec1() (*textCodec1, error) {
	this := new(textCodec1)
	this.logHashSize = TC_LOG_HASHES_SIZE
	this.dictSize = TC_THRESHOLD2 * 4
	this.dictMap = make([]*dictEntry, 1<<this.logHashSize)
	this.dictList = make([]dictEntry, this.dictSize)
	this.hashMask = int32(1<<this.logHashSize) - 1
	size := len(TC_STATIC_DICTIONARY)

	if size >= this.dictSize {
		size = this.dictSize
	}

	copy(this.dictList, TC_STATIC_DICTIONARY[0:size])
	nbWords := TC_STATIC_DICT_WORDS

	// Add special entries at end of static dictionary
	this.dictList[nbWords] = dictEntry{ptr: []byte{TC_ESCAPE_TOKEN2}, hash: 0, data: int32((1 << 24) | nbWords)}
	this.dictList[nbWords+1] = dictEntry{ptr: []byte{TC_ESCAPE_TOKEN1}, hash: 0, data: int32((1 << 24) | (nbWords + 1))}
	this.staticDictSize = nbWords + 2
	return this, nil
}

func newTextCodec1WithCtx(ctx *map[string]interface{}) (*textCodec1, error) {
	this := new(textCodec1)
	log := uint32(8)
	blockSize := uint(0)

	if val, containsKey := (*ctx)["size"]; containsKey {
		// Actual block size
		blockSize = val.(uint)

		if blockSize >= 1<<28 {
			log = 26
		} else if blockSize >= 1<<10 {
			log, _ = kanzi.Log2(uint32(blockSize / 4))
		}
	}

	// Select an appropriate initial dictionary size
	dSize := 1 << 12

	for i := uint(14); i <= 24; i += 2 {
		if blockSize >= 1<<i {
			dSize <<= 1
		}
	}

	extraMem := uint(0)

	if val, containsKey := (*ctx)["extra"]; containsKey {
		if val.(bool) == true {
			extraMem = 1
		}
	}

	this.logHashSize = uint(log) + extraMem
	this.dictSize = dSize
	this.dictMap = make([]*dictEntry, 1<<this.logHashSize)
	this.dictList = make([]dictEntry, this.dictSize)
	this.hashMask = int32(1<<this.logHashSize) - 1
	size := len(TC_STATIC_DICTIONARY)

	if size >= this.dictSize {
		size = this.dictSize
	}

	copy(this.dictList, TC_STATIC_DICTIONARY[0:size])
	nbWords := TC_STATIC_DICT_WORDS

	// Add special entries at end of static dictionary
	this.dictList[nbWords] = dictEntry{ptr: []byte{TC_ESCAPE_TOKEN2}, hash: 0, data: int32((1 << 24) | (nbWords))}
	this.dictList[nbWords+1] = dictEntry{ptr: []byte{TC_ESCAPE_TOKEN1}, hash: 0, data: int32((1 << 24) | (nbWords + 1))}
	this.staticDictSize = nbWords + 2
	return this, nil
}

func (this *textCodec1) reset() {
	// Clear and populate hash map
	for i := range this.dictMap {
		this.dictMap[i] = nil
	}

	for i := range this.dictList[0:this.staticDictSize] {
		e := this.dictList[i]
		this.dictMap[e.hash&this.hashMask] = &e
	}

	// Pre-allocate all dictionary entries
	for i := this.staticDictSize; i < this.dictSize; i++ {
		this.dictList[i] = dictEntry{ptr: nil, hash: 0, data: int32(i)}
	}
}

func (this *textCodec1) Forward(src, dst []byte) (uint, uint, error) {
	count := len(src)

	if n := this.MaxEncodedLen(count); len(dst) < n {
		return 0, 0, fmt.Errorf("Output buffer is too small - size: %d, required %d", len(dst), n)
	}

	srcIdx := 0
	dstIdx := 0
	freqs0 := [256]int32{}
	mode := computeStats(src[0:count], freqs0[:])

	// Not text ?
	if mode&TC_MASK_NOT_TEXT != 0 {
		return uint(srcIdx), uint(dstIdx), errors.New("Input is not text, skipping")
	}

	this.reset()
	srcEnd := count
	dstEnd := this.MaxEncodedLen(count)
	dstEnd4 := dstEnd - 4
	emitAnchor := 0 // never negative
	words := this.staticDictSize

	// DOS encoded end of line (CR+LF) ?
	this.isCRLF = mode&TC_MASK_CRLF != 0
	dst[dstIdx] = mode
	dstIdx++
	var err error

	for srcIdx < srcEnd && src[srcIdx] == ' ' {
		dst[dstIdx] = ' '
		srcIdx++
		dstIdx++
		emitAnchor++
	}

	var delimAnchor int // previous delimiter

	if isText(src[srcIdx]) {
		delimAnchor = srcIdx - 1
	} else {
		delimAnchor = srcIdx
	}

	for srcIdx < srcEnd {
		cur := src[srcIdx]

		// Should be 'if isText(cur) {', but compiler (1.11) issues slow code (bad inlining?)
		if isLowerCase(cur) || isUpperCase(cur) {
			srcIdx++
			continue
		}

		if (srcIdx > delimAnchor+2) && isDelimiter(cur) { // At least 2 letters
			// Compute hashes
			// h1 -> hash of word chars
			// h2 -> hash of word chars with first char case flipped
			val := src[delimAnchor+1]
			h1 := TC_HASH1
			h1 = h1*TC_HASH1 ^ int32(val)*TC_HASH2
			var caseFlag int32

			if isUpperCase(val) {
				caseFlag = 32
			} else {
				caseFlag = -32
			}

			h2 := TC_HASH1
			h2 = h2*TC_HASH1 ^ (int32(val)+caseFlag)*TC_HASH2

			for i := delimAnchor + 2; i < srcIdx; i++ {
				h := int32(src[i]) * TC_HASH2
				h1 = h1*TC_HASH1 ^ h
				h2 = h2*TC_HASH1 ^ h
			}

			// Check word in dictionary
			length := int32(srcIdx - delimAnchor - 1)
			pe1 := this.dictMap[h1&this.hashMask]

			// Check for hash collisions
			if (pe1 != nil) && (pe1.hash != h1 || pe1.data>>24 != length) {
				pe1 = nil
			}

			pe := pe1

			if pe == nil {
				if pe2 := this.dictMap[h2&this.hashMask]; pe2 != nil && pe2.data>>24 == length && pe2.hash == h2 {
					pe = pe2
				}
			}

			if pe != nil {
				if !sameWords(pe.ptr[1:length], src[delimAnchor+2:]) {
					pe = nil
				}
			}

			if pe == nil {
				// Word not found in the dictionary or hash collision: add or replace word
				if ((length > 3) || (length > 2 && words < TC_THRESHOLD2)) && length < TC_MAX_WORD_LENGTH {
					pe = &this.dictList[words]

					if int(pe.data&0x00FFFFFF) >= this.staticDictSize {
						// Evict and reuse old entry
						this.dictMap[pe.hash&this.hashMask] = nil
						pe.ptr = src[delimAnchor+1:]
						pe.hash = h1
						pe.data = (length << 24) | int32(words)
					}

					// Update hash map
					this.dictMap[h1&this.hashMask] = pe
					words++

					// Dictionary full ? Expand or reset index to end of static dictionary
					if words >= this.dictSize {
						if this.expandDictionary() == false {
							words = this.staticDictSize
						}
					}
				}
			} else {
				// Word found in the dictionary
				// Skip space if only delimiter between 2 word references
				if (emitAnchor != delimAnchor) || (src[delimAnchor] != ' ') {
					dIdx := this.emitSymbols(src[emitAnchor:delimAnchor+1], dst[dstIdx:dstEnd])

					if dIdx < 0 {
						err = errors.New("Text transform failed. Output buffer too small")
						break
					}

					dstIdx += dIdx
				}

				if dstIdx >= dstEnd4 {
					err = errors.New("Text transform failed. Output buffer too small")
					break
				}

				if pe == pe1 {
					dst[dstIdx] = TC_ESCAPE_TOKEN1
				} else {
					dst[dstIdx] = TC_ESCAPE_TOKEN2
				}

				dstIdx++
				dstIdx += emitWordIndex1(dst[dstIdx:dstIdx+3], int(pe.data&0x00FFFFFF))
				emitAnchor = delimAnchor + 1 + int(pe.data>>24)
			}
		}

		// Reset delimiter position
		delimAnchor = srcIdx
		srcIdx++
	}

	if err == nil {
		// Emit last symbols
		dIdx := this.emitSymbols(src[emitAnchor:srcEnd], dst[dstIdx:dstEnd])

		if dIdx < 0 {
			err = errors.New("Text transform failed. Output buffer too small")
		} else {
			dstIdx += dIdx
		}
	}

	if err == nil && srcIdx != srcEnd {
		err = fmt.Errorf("Text transform failed. Source index: %v, expected: %v", srcIdx, srcEnd)
	}

	return uint(srcIdx), uint(dstIdx), err
}

func (this *textCodec1) expandDictionary() bool {
	if this.dictSize >= TC_MAX_DICT_SIZE {
		return false
	}

	this.dictList = append(this.dictList, make([]dictEntry, this.dictSize)...)

	for i := this.dictSize; i < this.dictSize*2; i++ {
		this.dictList[i] = dictEntry{ptr: nil, hash: 0, data: int32(i)}
	}

	this.dictSize <<= 1
	return true
}

func (this *textCodec1) emitSymbols(src, dst []byte) int {
	dstIdx := 0
	dstEnd := len(dst)

	for i := range src {
		if dstIdx >= dstEnd {
			return -1
		}

		cur := src[i]

		switch cur {
		case TC_ESCAPE_TOKEN1:
			fallthrough
		case TC_ESCAPE_TOKEN2:
			// Emit special word
			dst[dstIdx] = TC_ESCAPE_TOKEN1
			dstIdx++
			var idx int
			lenIdx := 2

			if cur == TC_ESCAPE_TOKEN1 {
				idx = this.staticDictSize - 1
			} else {
				idx = this.staticDictSize - 2
			}

			if idx >= TC_THRESHOLD2 {
				lenIdx = 3
			} else if idx < TC_THRESHOLD1 {
				lenIdx = 1
			}

			if dstIdx+lenIdx >= dstEnd {
				return -1
			}

			dstIdx += emitWordIndex1(dst[dstIdx:dstIdx+lenIdx], idx)

		case CR:
			if this.isCRLF == false {
				dst[dstIdx] = cur
				dstIdx++
			}

		default:
			dst[dstIdx] = cur
			dstIdx++
		}
	}

	return dstIdx
}

func emitWordIndex1(dst []byte, val int) int {
	// Emit word index (varint 5 bits + 7 bits + 7 bits)
	if val >= TC_THRESHOLD1 {
		if val >= TC_THRESHOLD2 {
			dst[0] = byte(0xE0 | (val >> 14))
			dst[1] = byte(0x80 | (val >> 7))
			dst[2] = byte(0x7F & val)
			return 3
		}

		dst[0] = byte(0x80 | (val >> 7))
		dst[1] = byte(0x7F & val)
		return 2
	}

	dst[0] = byte(val)
	return 1
}

func (this *textCodec1) Inverse(src, dst []byte) (uint, uint, error) {
	srcIdx := 0
	dstIdx := 0
	this.reset()
	srcEnd := len(src)
	dstEnd := len(dst)
	var delimAnchor int // previous delimiter

	if isText(src[srcIdx]) {
		delimAnchor = srcIdx - 1
	} else {
		delimAnchor = srcIdx
	}

	words := this.staticDictSize
	wordRun := false
	err := error(nil)
	this.isCRLF = src[srcIdx]&0x01 != 0
	srcIdx++

	for srcIdx < srcEnd && dstIdx < dstEnd {
		cur := src[srcIdx]

		if isText(cur) {
			dst[dstIdx] = cur
			srcIdx++
			dstIdx++
			continue
		}

		if (srcIdx > delimAnchor+2) && isDelimiter(cur) {
			h1 := TC_HASH1

			for i := delimAnchor + 1; i < srcIdx; i++ {
				h1 = h1*TC_HASH1 ^ int32(src[i])*TC_HASH2
			}

			// Lookup word in dictionary
			length := int32(srcIdx - delimAnchor - 1)
			pe := this.dictMap[h1&this.hashMask]

			// Check for hash collisions
			if pe != nil {
				if pe.hash != h1 || pe.data>>24 != length {
					pe = nil
				} else if !sameWords(pe.ptr[1:length], src[delimAnchor+2:]) {
					pe = nil
				}
			}

			if pe == nil {
				// Word not found in the dictionary or hash collision: add or replace word
				if ((length > 3) || (length > 2 && words < TC_THRESHOLD2)) && length < TC_MAX_WORD_LENGTH {
					pe = &this.dictList[words]

					if int(pe.data&0x00FFFFFF) >= this.staticDictSize {
						// Evict and reuse old entry
						this.dictMap[pe.hash&this.hashMask] = nil
						pe.ptr = src[delimAnchor+1:]
						pe.hash = h1
						pe.data = (length << 24) | int32(words)
					}

					this.dictMap[h1&this.hashMask] = pe
					words++

					// Dictionary full ? Expand or reset index to end of static dictionary
					if words >= this.dictSize {
						if this.expandDictionary() == false {
							words = this.staticDictSize
						}
					}
				}
			}
		}

		srcIdx++

		if cur == TC_ESCAPE_TOKEN1 || cur == TC_ESCAPE_TOKEN2 {
			// Word in dictionary
			// Read word index (varint 5 bits + 7 bits + 7 bits)
			idx := int(src[srcIdx])
			srcIdx++

			if idx >= 0x80 {
				idx &= 0x7F
				idx2 := int(src[srcIdx])
				srcIdx++

				if idx2 >= 0x80 {
					idx = ((idx & 0x1F) << 7) | (idx2 & 0x7F)
					idx2 = int(src[srcIdx])
					srcIdx++
				}

				idx = (idx << 7) | (idx2 & 0x7F)

				if idx >= this.dictSize {
					break
				}
			}

			pe := &this.dictList[idx]
			length := int(pe.data >> 24)
			buf := pe.ptr

			// Sanity check
			if buf == nil || dstIdx+length >= dstEnd {
				err = fmt.Errorf("Invalid input data")
				break
			}

			// Add space if only delimiter between 2 words (not an escaped delimiter)
			if wordRun == true && length > 1 {
				dst[dstIdx] = ' '
				dstIdx++
			}

			// Emit word
			if cur != TC_ESCAPE_TOKEN2 {
				copy(dst[dstIdx:], buf[0:length])
			} else {
				// Flip case of first character
				if isUpperCase(buf[0]) {
					dst[dstIdx] = buf[0] + 32
				} else {
					dst[dstIdx] = buf[0] - 32
				}

				copy(dst[dstIdx+1:], buf[1:length])
			}

			dstIdx += length

			if length > 1 {
				// Regular word entry
				wordRun = true
				delimAnchor = srcIdx
			} else {
				// Escape entry
				wordRun = false
				delimAnchor = srcIdx - 1
			}
		} else {
			wordRun = false
			delimAnchor = srcIdx - 1

			if (this.isCRLF == true) && (cur == LF) {
				dst[dstIdx] = CR
				dstIdx++
			}

			dst[dstIdx] = cur
			dstIdx++
		}
	}

	if err == nil && srcIdx != srcEnd {
		err = fmt.Errorf("Text transform failed. Source index: %v, expected: %v", srcIdx, srcEnd)
	}

	return uint(srcIdx), uint(dstIdx), err
}

func (this textCodec1) MaxEncodedLen(srcLen int) int {
	// Limit to 1 x srcLength and let the caller deal with
	// a failure when the output is too small
	return srcLen
}

func newTextCodec2() (*textCodec2, error) {
	this := new(textCodec2)
	this.logHashSize = TC_LOG_HASHES_SIZE
	this.dictSize = TC_THRESHOLD2 * 4
	this.dictMap = make([]*dictEntry, 1<<this.logHashSize)
	this.dictList = make([]dictEntry, this.dictSize)
	this.hashMask = int32(1<<this.logHashSize) - 1
	size := len(TC_STATIC_DICTIONARY)

	if size >= this.dictSize {
		size = this.dictSize
	}

	copy(this.dictList, TC_STATIC_DICTIONARY[0:size])
	this.staticDictSize = TC_STATIC_DICT_WORDS
	return this, nil
}

func newTextCodec2WithCtx(ctx *map[string]interface{}) (*textCodec2, error) {
	this := new(textCodec2)
	log := uint32(8)
	blockSize := uint(0)

	if val, containsKey := (*ctx)["size"]; containsKey {
		// Actual block size
		blockSize = val.(uint)

		if blockSize >= 1<<28 {
			log = 26
		} else if blockSize >= 1<<10 {
			log, _ = kanzi.Log2(uint32(blockSize / 4))
		}
	}

	// Select an appropriate initial dictionary size
	dSize := 1 << 12

	for i := uint(14); i <= 24; i += 2 {
		if blockSize >= 1<<i {
			dSize <<= 1
		}
	}

	extraMem := uint(0)

	if val, containsKey := (*ctx)["extra"]; containsKey {
		if val.(bool) == true {
			extraMem = 1
		}
	}

	this.logHashSize = uint(log) + extraMem
	this.dictSize = dSize
	this.dictMap = make([]*dictEntry, 1<<this.logHashSize)
	this.dictList = make([]dictEntry, this.dictSize)
	this.hashMask = int32(1<<this.logHashSize) - 1
	size := len(TC_STATIC_DICTIONARY)

	if size >= this.dictSize {
		size = this.dictSize
	}

	copy(this.dictList, TC_STATIC_DICTIONARY[0:size])
	this.staticDictSize = TC_STATIC_DICT_WORDS
	return this, nil
}

func (this *textCodec2) reset() {
	// Clear and populate hash map
	for i := range this.dictMap {
		this.dictMap[i] = nil
	}

	for i := range this.dictList[0:this.staticDictSize] {
		e := this.dictList[i]
		this.dictMap[e.hash&this.hashMask] = &e
	}

	// Pre-allocate all dictionary entries
	for i := this.staticDictSize; i < this.dictSize; i++ {
		this.dictList[i] = dictEntry{ptr: nil, hash: 0, data: int32(i)}
	}
}

func (this *textCodec2) Forward(src, dst []byte) (uint, uint, error) {
	count := len(src)

	if n := this.MaxEncodedLen(count); len(dst) < n {
		return 0, 0, fmt.Errorf("Output buffer is too small - size: %d, required %d", len(dst), n)
	}

	srcIdx := 0
	dstIdx := 0
	freqs0 := [256]int32{}
	mode := computeStats(src[0:count], freqs0[:])

	// Not text ?
	if mode&TC_MASK_NOT_TEXT != 0 {
		return uint(srcIdx), uint(dstIdx), errors.New("Input is not text, skipping")
	}

	this.reset()
	srcEnd := count
	dstEnd := this.MaxEncodedLen(count)
	dstEnd3 := dstEnd - 3
	emitAnchor := 0 // never negative
	words := this.staticDictSize

	// DOS encoded end of line (CR+LF) ?
	this.isCRLF = mode&TC_MASK_CRLF != 0
	dst[dstIdx] = mode
	dstIdx++
	var err error

	for srcIdx < srcEnd && src[srcIdx] == ' ' {
		dst[dstIdx] = ' '
		srcIdx++
		dstIdx++
		emitAnchor++
	}

	var delimAnchor int // previous delimiter

	if isText(src[srcIdx]) {
		delimAnchor = srcIdx - 1
	} else {
		delimAnchor = srcIdx
	}

	for srcIdx < srcEnd {
		cur := src[srcIdx]

		// Should be 'if isText(cur) {', but compiler (1.11) issues slow code (bad inlining?)
		if isLowerCase(cur) || isUpperCase(cur) {
			srcIdx++
			continue
		}

		if (srcIdx > delimAnchor+2) && isDelimiter(cur) { // At least 2 letters
			// Compute hashes
			// h1 -> hash of word chars
			// h2 -> hash of word chars with first char case flipped
			val := src[delimAnchor+1]
			h1 := TC_HASH1
			h1 = h1*TC_HASH1 ^ int32(val)*TC_HASH2
			var caseFlag int32

			if isUpperCase(val) {
				caseFlag = 32
			} else {
				caseFlag = -32
			}

			h2 := TC_HASH1
			h2 = h2*TC_HASH1 ^ (int32(val)+caseFlag)*TC_HASH2

			for i := delimAnchor + 2; i < srcIdx; i++ {
				h := int32(src[i]) * TC_HASH2
				h1 = h1*TC_HASH1 ^ h
				h2 = h2*TC_HASH1 ^ h
			}

			// Check word in dictionary
			length := int32(srcIdx - delimAnchor - 1)
			pe1 := this.dictMap[h1&this.hashMask]

			// Check for hash collisions
			if (pe1 != nil) && (pe1.hash != h1 || pe1.data>>24 != length) {
				pe1 = nil
			}

			pe := pe1

			if pe == nil {
				if pe2 := this.dictMap[h2&this.hashMask]; pe2 != nil && pe2.data>>24 == length && pe2.hash == h2 {
					pe = pe2
				}
			}

			if pe != nil {
				if !sameWords(pe.ptr[1:length], src[delimAnchor+2:]) {
					pe = nil
				}
			}

			if pe == nil {
				// Word not found in the dictionary or hash collision: add or replace word
				if ((length > 3) || (length > 2 && words < TC_THRESHOLD2)) && length < TC_MAX_WORD_LENGTH {
					pe = &this.dictList[words]

					if int(pe.data&0x00FFFFFF) >= this.staticDictSize {
						// Evict and reuse old entry
						this.dictMap[pe.hash&this.hashMask] = nil
						pe.ptr = src[delimAnchor+1:]
						pe.hash = h1
						pe.data = (length << 24) | int32(words)
					}

					// Update hash map
					this.dictMap[h1&this.hashMask] = pe
					words++

					// Dictionary full ? Expand or reset index to end of static dictionary
					if words >= this.dictSize {
						if this.expandDictionary() == false {
							words = this.staticDictSize
						}
					}
				}
			} else {
				// Word found in the dictionary
				// Skip space if only delimiter between 2 word references
				if (emitAnchor != delimAnchor) || (src[delimAnchor] != ' ') {
					dIdx := this.emitSymbols(src[emitAnchor:delimAnchor+1], dst[dstIdx:dstEnd])

					if dIdx < 0 {
						err = errors.New("Text transform failed. Output buffer too small")
						break
					}

					dstIdx += dIdx
				}

				if dstIdx >= dstEnd3 {
					err = errors.New("Text transform failed. Output buffer too small")
					break
				}

				mask := 0

				if pe != pe1 {
					mask = 32
				}

				dstIdx += emitWordIndex2(dst[dstIdx:dstIdx+3], int(pe.data&0x00FFFFFF), mask)
				emitAnchor = delimAnchor + 1 + int(pe.data>>24)
			}
		}

		// Reset delimiter position
		delimAnchor = srcIdx
		srcIdx++
	}

	if err == nil {
		// Emit last symbols
		dIdx := this.emitSymbols(src[emitAnchor:srcEnd], dst[dstIdx:dstEnd])

		if dIdx < 0 {
			err = errors.New("Text transform failed. Output buffer too small")
		} else {
			dstIdx += dIdx
		}
	}

	if err == nil && srcIdx != srcEnd {
		err = fmt.Errorf("Text transform failed. Source index: %v, expected: %v", srcIdx, srcEnd)
	}

	return uint(srcIdx), uint(dstIdx), err
}

func (this *textCodec2) expandDictionary() bool {
	if this.dictSize >= TC_MAX_DICT_SIZE {
		return false
	}

	this.dictList = append(this.dictList, make([]dictEntry, this.dictSize)...)

	for i := this.dictSize; i < this.dictSize*2; i++ {
		this.dictList[i] = dictEntry{ptr: nil, hash: 0, data: int32(i)}
	}

	this.dictSize <<= 1
	return true
}

func (this *textCodec2) emitSymbols(src, dst []byte) int {
	dstIdx := 0

	if 2*len(src) < len(dst) {
		for i := range src {
			cur := src[i]

			switch cur {
			case TC_ESCAPE_TOKEN1:
				dst[dstIdx] = TC_ESCAPE_TOKEN1
				dstIdx++
				dst[dstIdx] = TC_ESCAPE_TOKEN1
				dstIdx++

			case CR:
				if this.isCRLF == false {
					dst[dstIdx] = cur
					dstIdx++
				}

			default:
				if cur&0x80 != 0 {
					dst[dstIdx] = TC_ESCAPE_TOKEN1
					dstIdx++
				}

				dst[dstIdx] = cur
				dstIdx++
			}
		}
	} else {
		for i := range src {
			cur := src[i]

			switch cur {
			case TC_ESCAPE_TOKEN1:
				if dstIdx+1 >= len(dst) {
					return -1
				}

				dst[dstIdx] = TC_ESCAPE_TOKEN1
				dstIdx++
				dst[dstIdx] = TC_ESCAPE_TOKEN1
				dstIdx++

			case CR:
				if this.isCRLF == false {
					if dstIdx >= len(dst) {
						return -1
					}

					dst[dstIdx] = cur
					dstIdx++
				}

			default:
				if cur&0x80 != 0 {
					if dstIdx >= len(dst) {
						return -1
					}

					dst[dstIdx] = TC_ESCAPE_TOKEN1
					dstIdx++
				}

				if dstIdx >= len(dst) {
					return -1
				}

				dst[dstIdx] = cur
				dstIdx++
			}
		}
	}

	return dstIdx
}

func emitWordIndex2(dst []byte, val, mask int) int {
	// Emit word index (varint 5 bits + 7 bits + 7 bits)
	// 1st byte: 0x80 => word idx, 0x40 => more bytes, 0x20 => toggle case 1st symbol
	// 2nd byte: 0x80 => 1 more byte
	if val >= TC_THRESHOLD3 {
		if val >= TC_THRESHOLD4 {
			// 5 + 7 + 7 => 2^19
			dst[0] = byte(0xC0 | mask | ((val >> 14) & 0x1F))
			dst[1] = byte(0x80 | ((val >> 7) & 0x7F))
			dst[2] = byte(val & 0x7F)
			return 3
		}

		// 5 + 7 => 2^12 = 32*128
		dst[0] = byte(0xC0 | mask | ((val >> 7) & 0x1F))
		dst[1] = byte(val & 0x7F)
		return 2
	}

	dst[0] = byte(0x80 | mask | val)
	return 1
}

func (this *textCodec2) Inverse(src, dst []byte) (uint, uint, error) {
	srcIdx := 0
	dstIdx := 0
	this.reset()
	srcEnd := len(src)
	dstEnd := len(dst)
	var delimAnchor int // previous delimiter

	if isText(src[srcIdx]) {
		delimAnchor = srcIdx - 1
	} else {
		delimAnchor = srcIdx
	}

	words := this.staticDictSize
	wordRun := false
	err := error(nil)
	this.isCRLF = src[srcIdx]&0x01 != 0
	srcIdx++

	for srcIdx < srcEnd && dstIdx < dstEnd {
		cur := src[srcIdx]

		if isText(cur) {
			dst[dstIdx] = cur
			srcIdx++
			dstIdx++
			continue
		}

		if (srcIdx > delimAnchor+2) && isDelimiter(cur) {
			h1 := TC_HASH1

			for i := delimAnchor + 1; i < srcIdx; i++ {
				h1 = h1*TC_HASH1 ^ int32(src[i])*TC_HASH2
			}

			// Lookup word in dictionary
			length := int32(srcIdx - delimAnchor - 1)
			pe := this.dictMap[h1&this.hashMask]

			// Check for hash collisions
			if pe != nil {
				if pe.hash != h1 || pe.data>>24 != length {
					pe = nil
				} else if !sameWords(pe.ptr[1:length], src[delimAnchor+2:]) {
					pe = nil
				}
			}

			if pe == nil {
				// Word not found in the dictionary or hash collision: add or replace word
				if ((length > 3) || (length > 2 && words < TC_THRESHOLD2)) && length < TC_MAX_WORD_LENGTH {
					pe = &this.dictList[words]

					if int(pe.data&0x00FFFFFF) >= this.staticDictSize {
						// Evict and reuse old entry
						this.dictMap[pe.hash&this.hashMask] = nil
						pe.ptr = src[delimAnchor+1:]
						pe.hash = h1
						pe.data = (length << 24) | int32(words)
					}

					this.dictMap[h1&this.hashMask] = pe
					words++

					// Dictionary full ? Expand or reset index to end of static dictionary
					if words >= this.dictSize {
						if this.expandDictionary() == false {
							words = this.staticDictSize
						}
					}
				}
			}
		}

		srcIdx++

		if cur&0x80 != 0 {
			// Word in dictionary
			// Read word index (varint 5 bits + 7 bits + 7 bits)
			idx := int(cur & 0x1F)

			if cur&0x40 != 0 {
				idx2 := int(src[srcIdx])
				srcIdx++

				if idx2&0x80 != 0 {
					idx = (idx << 7) | (idx2 & 0x7F)
					idx2 = int(src[srcIdx])
					srcIdx++
				}

				idx = (idx << 7) | (idx2 & 0x7F)

				if idx >= this.dictSize {
					break
				}
			}

			pe := &this.dictList[idx]
			length := int(pe.data >> 24)
			buf := pe.ptr

			// Sanity check
			if buf == nil || dstIdx+length >= dstEnd {
				err = fmt.Errorf("Invalid input data")
				break
			}

			// Add space if only delimiter between 2 words (not an escaped delimiter)
			if wordRun == true && length > 1 {
				dst[dstIdx] = ' '
				dstIdx++
			}

			// Emit word
			if cur&0x20 == 0 {
				copy(dst[dstIdx:], buf[0:length])
			} else {
				// Flip case of first character
				if isUpperCase(buf[0]) {
					dst[dstIdx] = buf[0] + 32
				} else {
					dst[dstIdx] = buf[0] - 32
				}

				copy(dst[dstIdx+1:], buf[1:length])
			}

			dstIdx += length

			if length > 1 {
				// Regular word entry
				wordRun = true
				delimAnchor = srcIdx
			} else {
				// Escape entry
				wordRun = false
				delimAnchor = srcIdx - 1
			}
		} else {

			if cur == TC_ESCAPE_TOKEN1 {
				dst[dstIdx] = src[srcIdx]
				srcIdx++
				dstIdx++
			} else {
				if (this.isCRLF == true) && (cur == LF) {
					dst[dstIdx] = CR
					dstIdx++
				}

				dst[dstIdx] = cur
				dstIdx++
			}

			wordRun = false
			delimAnchor = srcIdx - 1
		}
	}

	if err == nil && srcIdx != srcEnd {
		err = fmt.Errorf("Text transform failed. Source index: %v, expected: %v", srcIdx, srcEnd)
	}

	return uint(srcIdx), uint(dstIdx), err
}

func (this textCodec2) MaxEncodedLen(srcLen int) int {
	// Limit to 1 x srcLength and let the caller deal with
	// a failure when the output is too small
	return srcLen
}
