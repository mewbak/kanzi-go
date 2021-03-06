/*
Copyright 2011-2017 Frederic Langlet
Licensed under the Apache License, Version 2.0 (the "License");
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
)

// X86Codec is a codec that replaces relative jumps addresses with
// absolute ones in X86 code (to improve entropy coding).
// Adapted from MCM: https://github.com/mathieuchartier/mcm/blob/master/X86Binary.hpp

const (
	_X86_INSTRUCTION_MASK = 0xFE
	_X86_INSTRUCTION_JUMP = 0xE8
	_X86_ADDRESS_MASK     = 0xD5
	_X86_ESCAPE           = 0x02
)

// X86Codec a codec for x86 code
type X86Codec struct {
}

// NewX86Codec creates a new instance of X86Codec
func NewX86Codec() (*X86Codec, error) {
	this := &X86Codec{}
	return this, nil
}

// NewX86CodecWithCtx creates a new instance of X86Codec using a
// configuration map as parameter.
func NewX86CodecWithCtx(ctx *map[string]interface{}) (*X86Codec, error) {
	this := &X86Codec{}
	return this, nil
}

// Forward applies the function to the src and writes the result
// to the destination. Returns number of bytes read, number of bytes
// written and possibly an error. If the source data does not represent
// X86 code, an error is returned.
func (this *X86Codec) Forward(src, dst []byte) (uint, uint, error) {
	if &src[0] == &dst[0] {
		return 0, 0, errors.New("Input and output buffers cannot be equal")
	}

	count := len(src)

	if n := this.MaxEncodedLen(count); len(dst) < n {
		return 0, 0, fmt.Errorf("Output buffer is too small - size: %d, required %d", len(dst), n)
	}

	jumps := 0
	end := count - 8

	for i := 0; i < end; i++ {
		if src[i]&_X86_INSTRUCTION_MASK == _X86_INSTRUCTION_JUMP {
			// Count valid relative jumps (E8/E9 .. .. .. 00/FF)
			if src[i+4] == 0 || src[i+4] == 255 {
				// No encoding conflict ?
				if src[i] != 0 && src[i] != 1 && src[i] != _X86_ESCAPE {
					jumps++
				}
			}
		}
	}

	if jumps < (count >> 7) {
		// Number of jump instructions too small => either not a binary
		// or not worth the change => skip. Very crude filter obviously.
		// Also, binaries usually have a lot of 0x88..0x8C (MOV) instructions.
		return 0, 0, errors.New("Not a binary or not enough jumps")
	}

	srcIdx := 0
	dstIdx := 0

	for srcIdx < end {
		dst[dstIdx] = src[srcIdx]
		dstIdx++
		srcIdx++

		// Relative jump ?
		if src[srcIdx-1]&_X86_INSTRUCTION_MASK != _X86_INSTRUCTION_JUMP {
			continue
		}

		cur := src[srcIdx]

		if cur == 0 || cur == 1 || cur == _X86_ESCAPE {
			// Conflict prevents encoding the address. Emit escape symbol
			dst[dstIdx] = _X86_ESCAPE
			dst[dstIdx+1] = cur
			srcIdx++
			dstIdx += 2
			continue
		}

		sgn := src[srcIdx+3]

		// Invalid sign of jump address difference => false positive ?
		if sgn != 0 && sgn != 255 {
			continue
		}

		addr := int32(src[srcIdx]) | (int32(src[srcIdx+1]) << 8) |
			(int32(src[srcIdx+2]) << 16) | (int32(sgn) << 24)

		addr += int32(srcIdx)
		dst[dstIdx] = sgn + 1
		dst[dstIdx+1] = _X86_ADDRESS_MASK ^ byte(addr>>16)
		dst[dstIdx+2] = _X86_ADDRESS_MASK ^ byte(addr>>8)
		dst[dstIdx+3] = _X86_ADDRESS_MASK ^ byte(addr)
		srcIdx += 4
		dstIdx += 4
	}

	for srcIdx < count {
		dst[dstIdx] = src[srcIdx]
		dstIdx++
		srcIdx++
	}

	return uint(srcIdx), uint(dstIdx), nil
}

// Inverse applies the reverse function to the src and writes the result
// to the destination. Returns number of bytes read, number of bytes
// written and possibly an error.
func (this *X86Codec) Inverse(src, dst []byte) (uint, uint, error) {
	if &src[0] == &dst[0] {
		return 0, 0, errors.New("Input and output buffers cannot be equal")
	}

	count := len(src)
	srcIdx := 0
	dstIdx := 0
	end := count - 8

	for srcIdx < end {
		dst[dstIdx] = src[srcIdx]
		dstIdx++
		srcIdx++

		// Relative jump ?
		if src[srcIdx-1]&_X86_INSTRUCTION_MASK != _X86_INSTRUCTION_JUMP {
			continue
		}

		sgn := src[srcIdx]

		if sgn == _X86_ESCAPE {
			// Not an encoded address. Skip escape symbol
			srcIdx++
			continue
		}

		// Invalid sign of jump address difference => false positive ?
		if sgn != 1 && sgn != 0 {
			continue
		}

		addr := (_X86_ADDRESS_MASK ^ int32(src[srcIdx+3])) |
			((_X86_ADDRESS_MASK ^ int32(src[srcIdx+2])) << 8) |
			((_X86_ADDRESS_MASK ^ int32(src[srcIdx+1])) << 16) |
			((0xFF & int32(sgn-1)) << 24)

		addr -= int32(dstIdx)
		dst[dstIdx] = byte(addr)
		dst[dstIdx+1] = byte(addr >> 8)
		dst[dstIdx+2] = byte(addr >> 16)
		dst[dstIdx+3] = byte(sgn - 1)
		srcIdx += 4
		dstIdx += 4
	}

	for srcIdx < count {
		dst[dstIdx] = src[srcIdx]
		dstIdx++
		srcIdx++
	}

	return uint(srcIdx), uint(dstIdx), nil
}

// MaxEncodedLen returns the max size required for the encoding output buffer
func (this X86Codec) MaxEncodedLen(srcLen int) int {
	// Since we do not check the dst index for each byte (for speed purpose)
	// allocate some extra buffer for incompressible data.
	if srcLen >= 1<<30 {
		return srcLen
	}

	if srcLen <= 512 {
		return srcLen + 32
	}

	return srcLen + srcLen/16
}
