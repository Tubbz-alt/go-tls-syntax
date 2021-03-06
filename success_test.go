package syntax

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func unhex(encoded string) []byte {
	decoded, err := hex.DecodeString(encoded)
	if err != nil {
		panic(err)
	}
	return decoded
}

func buffer(size int) []byte {
	return bytes.Repeat([]byte{0xA0}, size)
}

func hexBuffer(size int) string {
	return strings.Repeat("A0", size)
}

type CrypticString string

// A CrypticString marshalls as one length octet followed by the
// UTF-8 bytes of the string, XOR'ed with an increasing sequence
// starting with the length plus one (L+1, L+2, ...).
func (cs CrypticString) MarshalTLS() ([]byte, error) {
	l := byte(len(cs))
	b := []byte(cs)
	for i := range b {
		b[i] ^= l + byte(i) + 1
	}
	return append([]byte{l}, b...), nil
}

func (cs *CrypticString) UnmarshalTLS(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, fmt.Errorf("Length of CrypticString must be at least 1")
	}

	l := data[0]
	if len(data) < int(l)+1 {
		return 0, fmt.Errorf("TLS data not long enough for CrypticString")
	}

	b := data[1 : l+1]
	for i := range b {
		b[i] ^= l + byte(i) + 1
	}

	*cs = CrypticString(string(b))

	return int(l + 1), nil
}

func (cs CrypticString) ValidForTLS() error {
	if len(cs) > 256 {
		return fmt.Errorf("CrypticString length to large: %d", len(cs))
	}

	if string(cs) == "fnord" {
		return fmt.Errorf("Forbidden value")
	}

	return nil
}

func TestSuccessCases(t *testing.T) {
	dummyUint16 := uint16(0xFFFF)
	crypticHello := CrypticString("hello")
	testCases := map[string]struct {
		value    interface{}
		encoding []byte
	}{
		// Uints
		"uint8": {
			value:    uint8(0xA0),
			encoding: unhex("A0"),
		},
		"uint16": {
			value:    uint16(0xB0A0),
			encoding: unhex("B0A0"),
		},
		"uint32": {
			value:    uint32(0xD0C0B0A0),
			encoding: unhex("D0C0B0A0"),
		},
		"uint64": {
			value:    uint64(0xD0C0B0A090807060),
			encoding: unhex("D0C0B0A090807060"),
		},

		// Varints
		"varint8": {
			value: struct {
				V uint8 `tls:"varint"`
			}{V: 0x3F},
			encoding: unhex("3F"),
		},
		"varint16": {
			value: struct {
				V uint16 `tls:"varint"`
			}{V: 0x3FFF},
			encoding: unhex("7FFF"),
		},
		"varint32": {
			value: struct {
				V uint32 `tls:"varint"`
			}{V: 0x3FFFFFFF},
			encoding: unhex("BFFFFFFF"),
		},
		"varint64": {
			value: struct {
				V uint64 `tls:"varint"`
			}{V: 0x3FFFFFFFFFFFFFFF},
			encoding: unhex("FFFFFFFFFFFFFFFF"),
		},

		// Arrays
		"array": {
			value:    [5]uint16{0x0102, 0x0304, 0x0506, 0x0708, 0x090a},
			encoding: unhex("0102030405060708090a"),
		},

		// Slices
		"slice-0x20": {
			value: struct {
				V []byte `tls:"head=1"`
			}{
				V: buffer(0x20),
			},
			encoding: unhex("20" + hexBuffer(0x20)),
		},
		"slice-0x200": {
			value: struct {
				V []byte `tls:"head=2"`
			}{
				V: buffer(0x200),
			},
			encoding: unhex("0200" + hexBuffer(0x200)),
		},
		"slice-0x20000": {
			value: struct {
				V []byte `tls:"head=3"`
			}{
				V: buffer(0x20000),
			},
			encoding: unhex("020000" + hexBuffer(0x20000)),
		},
		"slice-none": {
			value: struct {
				V []byte `tls:"head=none"`
			}{
				V: buffer(0x3FFF),
			},
			encoding: unhex(hexBuffer(0x3FFF)),
		},
		"slice-varint": {
			value: struct {
				V []byte `tls:"head=varint"`
			}{
				V: buffer(0x3FFF),
			},
			encoding: unhex("7FFF" + hexBuffer(0x3FFF)),
		},

		// Maps
		"map": {
			value: struct {
				V map[uint16]uint8 `tls:"head=1"`
			}{
				V: map[uint16]uint8{2: 1, 1: 2},
			},
			encoding: unhex("06000102000201"),
		},

		// Struct
		"struct": {
			value: struct {
				A uint16
				B []uint8 `tls:"head=2"`
				C [4]uint32
			}{
				A: 0xB0A0,
				B: []uint8{0xA0, 0xA1, 0xA2, 0xA3, 0xA4},
				C: [4]uint32{0x10111213, 0x20212223, 0x30313233, 0x40414243},
			},
			encoding: unhex("B0A0" + "0005A0A1A2A3A4" + "10111213202122233031323340414243"),
		},
		"struct-omit": {
			value: struct {
				A uint16
				B []uint8 `tls:"omit"`
				C [4]uint32
			}{
				A: 0xB0A0,
				B: nil,
				C: [4]uint32{0x10111213, 0x20212223, 0x30313233, 0x40414243},
			},
			encoding: unhex("B0A0" + "10111213202122233031323340414243"),
		},
		"struct-pointer": {
			value:    struct{ V *uint16 }{V: &dummyUint16},
			encoding: unhex("FFFF"),
		},

		// Optional
		"optional-absent": {
			value: struct {
				A *uint16 `tls:"optional"`
			}{
				A: nil,
			},
			encoding: unhex("00"),
		},
		"optional-present": {
			value: struct {
				A *uint16 `tls:"optional"`
			}{
				A: &dummyUint16,
			},
			encoding: unhex("01FFFF"),
		},
		"optional-marshaler-absent": {
			value: struct {
				A *CrypticString `tls:"optional"`
			}{
				A: nil,
			},
			encoding: unhex("00"),
		},
		"optional-marshaler-present": {
			value: struct {
				A *CrypticString `tls:"optional"`
			}{
				A: &crypticHello,
			},
			encoding: unhex("01056e62646565"),
		},

		// Marshaler
		"marshaler": {
			value:    crypticHello,
			encoding: unhex("056e62646565"),
		},
		"struct-marshaler": {
			value: struct {
				A CrypticString
				B uint16
				C CrypticString
			}{
				A: CrypticString("hello"),
				B: 0xB0A0,
				C: CrypticString("... world!"),
			},
			encoding: unhex("056e62646565" + "B0A0" + "0a2522232e787f637e7735"),
		},
	}

	for label, testCase := range testCases {
		t.Run(label, func(t *testing.T) {
			// Test that encode succeeds
			encoding, err := Marshal(testCase.value)
			require.Nil(t, err)
			require.Equal(t, encoding, testCase.encoding)

			// Test that decode succeeds
			decodedPointer := reflect.New(reflect.TypeOf(testCase.value))
			read, err := Unmarshal(testCase.encoding, decodedPointer.Interface())
			require.Nil(t, err)
			require.Equal(t, read, len(encoding))

			decodedValue := decodedPointer.Elem().Interface()
			require.Equal(t, decodedValue, testCase.value)
		})
	}
}
