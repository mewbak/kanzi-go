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

// Implementation of an escaped RLE
// Run length encoding:
// RUN_LEN_ENCODE1 = 224 => RUN_LEN_ENCODE2 = 31*224 = 6944
// 4    <= runLen < 224+4      -> 1 byte
// 228  <= runLen < 6944+228   -> 2 bytes
// 7172 <= runLen < 65535+7172 -> 3 bytes

import (
	"errors"
	"fmt"

	kanzi "github.com/flanglet/kanzi-go"
)

const (
	_RLT_RUN_LEN_ENCODE1 = 224                               // used to encode run length
	_RLT_RUN_LEN_ENCODE2 = (255 - _RLT_RUN_LEN_ENCODE1) << 8 // used to encode run length
	_RLT_RUN_THRESHOLD   = 3
	_RLT_MAX_RUN         = 0xFFFF + _RLT_RUN_LEN_ENCODE2 + _RLT_RUN_THRESHOLD - 1
	_RLT_MAX_RUN4        = _RLT_MAX_RUN - 4
)

// RLT a Run Length Transform with escape symbol
type RLT struct {
}

// NewRLT creates a new instance of RLT
func NewRLT() (*RLT, error) {
	this := &RLT{}
	return this, nil
}

// NewRLTWithCtx creates a new instance of RLT using a
// configuration map as parameter.
func NewRLTWithCtx(ctx *map[string]interface{}) (*RLT, error) {
	this := &RLT{}
	return this, nil
}

// Forward applies the function to the src and writes the result
// to the destination. Returns number of bytes read, number of bytes
// written and possibly an error.
func (this *RLT) Forward(src, dst []byte) (uint, uint, error) {
	if len(src) == 0 {
		return 0, 0, nil
	}

	if &src[0] == &dst[0] {
		return 0, 0, errors.New("Input and output buffers cannot be equal")
	}

	if n := this.MaxEncodedLen(len(src)); len(dst) < n {
		return 0, 0, fmt.Errorf("Output buffer is too small - size: %d, required %d", len(dst), n)
	}

	srcIdx := 0
	dstIdx := 0
	srcEnd := len(src)
	srcEnd4 := srcEnd - 4
	dstEnd := len(dst)
	freqs := [256]int{}
	kanzi.ComputeHistogram(src[srcIdx:srcEnd], freqs[:], true, false)

	minIdx := 0

	// Select escape symbol
	if freqs[minIdx] > 0 {
		for i, f := range freqs {
			if f < freqs[minIdx] {
				minIdx = i

				if f == 0 {
					break
				}
			}
		}
	}

	escape := byte(minIdx)
	run := 0
	var err error
	prev := src[srcIdx]
	srcIdx++
	dst[dstIdx] = escape
	dstIdx++
	dst[dstIdx] = prev
	dstIdx++

	if prev == escape {
		dst[dstIdx] = 0
		dstIdx++
	}

	// Main loop
	for srcIdx < srcEnd4 {
		if prev == src[srcIdx] {
			srcIdx++
			run++

			if prev == src[srcIdx] {
				srcIdx++
				run++

				if prev == src[srcIdx] {
					srcIdx++
					run++

					if prev == src[srcIdx] {
						srcIdx++
						run++

						if run < _RLT_MAX_RUN4 {
							continue
						}
					}
				}
			}
		}

		if run > _RLT_RUN_THRESHOLD {
			dIdx, err2 := emitRunLength(dst[dstIdx:dstEnd], run, escape, prev)

			if err2 != nil {
				err = err2
				break
			}

			dstIdx += dIdx
		} else if prev != escape {
			if dstIdx+run >= dstEnd {
				err = errors.New("Output buffer is too small")
				break
			}

			if run > 0 {
				dst[dstIdx] = prev
				dstIdx++
				run--
			}

			for run > 0 {
				dst[dstIdx] = prev
				dstIdx++
				run--
			}
		} else { // escape literal
			if dstIdx+2*run >= dstEnd {
				err = errors.New("Output buffer is too small")
				break
			}

			for run > 0 {
				dst[dstIdx] = escape
				dst[dstIdx+1] = 0
				dstIdx += 2
				run--
			}
		}

		prev = src[srcIdx]
		srcIdx++
		run = 1
	}

	if err == nil {
		// Process any remaining run
		if run > _RLT_RUN_THRESHOLD {
			dIdx, err2 := emitRunLength(dst[dstIdx:dstEnd], run, escape, prev)

			if err2 != nil {
				err = err2
			} else {
				dstIdx += dIdx
			}
		} else if prev != escape {
			if dstIdx+run < dstEnd {
				for run > 0 {
					dst[dstIdx] = prev
					dstIdx++
					run--
				}
			}
		} else { // escape literal
			if dstIdx+2*run < dstEnd {
				for run > 0 {
					dst[dstIdx] = escape
					dst[dstIdx+1] = 0
					dstIdx += 2
					run--
				}
			}
		}

		// Copy the last few bytes
		for srcIdx < srcEnd && dstIdx < dstEnd {
			dst[dstIdx] = src[srcIdx]
			srcIdx++
			dstIdx++
		}

		if srcIdx != srcEnd {
			err = errors.New("Output buffer is too small")
		} else if dstIdx > srcIdx {
			err = errors.New("Input not compressed")
		}
	}

	return uint(srcIdx), uint(dstIdx), err
}

func emitRunLength(dst []byte, run int, escape, val byte) (int, error) {
	dst[0] = val
	dstIdx := 1

	if val == escape {
		dst[1] = 0
		dstIdx = 2
	}

	dst[dstIdx] = escape
	dstIdx++
	run -= _RLT_RUN_THRESHOLD

	// Encode run length
	if run >= _RLT_RUN_LEN_ENCODE1 {
		if run < _RLT_RUN_LEN_ENCODE2 {
			if dstIdx >= len(dst)-2 {
				return dstIdx, errors.New("Output buffer too small")
			}

			run -= _RLT_RUN_LEN_ENCODE1
			dst[dstIdx] = byte(_RLT_RUN_LEN_ENCODE1 + (run >> 8))
			dstIdx++
		} else {
			if dstIdx >= len(dst)-3 {
				return dstIdx, errors.New("Output buffer too small")
			}

			run -= _RLT_RUN_LEN_ENCODE2
			dst[dstIdx] = byte(0xFF)
			dst[dstIdx+1] = byte(run >> 8)
			dstIdx += 2
		}
	}

	dst[dstIdx] = byte(run)
	return dstIdx + 1, nil
}

// Inverse applies the reverse function to the src and writes the result
// to the destination. Returns number of bytes read, number of bytes
// written and possibly an error.
func (this *RLT) Inverse(src, dst []byte) (uint, uint, error) {
	if len(src) == 0 {
		return 0, 0, nil
	}

	if &src[0] == &dst[0] {
		return 0, 0, errors.New("Input and output buffers cannot be equal")
	}

	srcIdx := 0
	dstIdx := 0
	srcEnd := len(src)
	dstEnd := len(dst)
	escape := src[srcIdx]
	srcIdx++
	var err error

	if src[srcIdx] == escape {
		srcIdx++

		// The data cannot start with a run but may start with an escape literal
		if srcIdx < srcEnd && src[srcIdx] != 0 {
			return uint(srcIdx), uint(dstIdx), errors.New("Invalid input data: input starts with a run")
		}

		srcIdx++
		dst[dstIdx] = escape
		dstIdx++
	}

	// Main loop
	for srcIdx < srcEnd {
		if src[srcIdx] != escape {
			// Literal
			if dstIdx >= dstEnd {
				err = errors.New("Invalid input data")
				break
			}

			dst[dstIdx] = src[srcIdx]
			srcIdx++
			dstIdx++
			continue
		}

		srcIdx++

		if srcIdx >= srcEnd {
			err = errors.New("Invalid input data")
			break
		}

		val := dst[dstIdx-1]
		run := int(src[srcIdx])
		srcIdx++

		if run == 0 {
			// Just an escape symbol, not a run
			if dstIdx >= dstEnd {
				err = errors.New("Invalid input data")
				break
			}

			dst[dstIdx] = escape
			dstIdx++
			continue
		}

		// Decode the length
		if run == 0xFF {
			if srcIdx+1 >= srcEnd {
				err = errors.New("Invalid input data")
				break
			}

			run = (int(src[srcIdx]) << 8) | int(src[srcIdx+1])
			srcIdx += 2
			run += _RLT_RUN_LEN_ENCODE2
		} else if run >= _RLT_RUN_LEN_ENCODE1 {
			if srcIdx >= srcEnd {
				err = errors.New("Invalid input data")
				break
			}

			run = ((run - _RLT_RUN_LEN_ENCODE1) << 8) | int(src[srcIdx])
			run += _RLT_RUN_LEN_ENCODE1
			srcIdx++
		}

		run += (_RLT_RUN_THRESHOLD - 1)

		// Sanity check
		if dstIdx+run >= dstEnd || run > _RLT_MAX_RUN {
			err = errors.New("Invalid run length")
			break
		}

		// Emit 'run' times the previous byte
		for run >= 4 {
			dst[dstIdx] = val
			dst[dstIdx+1] = val
			dst[dstIdx+2] = val
			dst[dstIdx+3] = val
			dstIdx += 4
			run -= 4
		}

		for run > 0 {
			dst[dstIdx] = val
			dstIdx++
			run--
		}
	}

	if srcIdx != srcEnd && err == nil {
		err = errors.New("Invalid input data")
	}

	return uint(srcIdx), uint(dstIdx), err
}

// MaxEncodedLen returns the max size required for the encoding output buffer
func (this RLT) MaxEncodedLen(srcLen int) int {
	if srcLen <= 512 {
		return srcLen + 32
	}

	return srcLen
}
