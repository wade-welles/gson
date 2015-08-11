// Package cbor implements RFC-7049 to encode golang data into
// binary format and vice-versa.
//
// Following golang native types are supported,
//
//   * nil, true, false.
//   * native integer types, and its alias, of all width.
//   * float32, float64.
//   * slice of bytes.
//   * native string.
//   * slice of interface - []interface{}.
//   * map of string to interface{} - map[string]interface{}.
//
// Custom types defined by this package can also be encoded using cbor.
//
//   * Undefined - to encode a data-item as undefined.
package cbor

import "math"
import "math/big"
import "regexp"
import "time"
import "errors"
import "encoding/binary"

// ErrorDecodeFloat16 cannot decode float16.
var ErrorDecodeFloat16 = errors.New("cbor.decodeFloat16")

// ErrorDecodeExceedInt64 cannot decode float16.
var ErrorDecodeExceedInt64 = errors.New("cbor.decodeExceedInt64")

const ( // major types.
	type0 byte = iota << 5 // unsigned integer
	type1                  // negative integer
	type2                  // byte string
	type3                  // text string
	type4                  // array
	type5                  // map
	type6                  // tagged data-item
	type7                  // floating-point, simple-types and break-stop
)

const ( // associated information for type0 and type1.
	// 0..23 actual value
	info24 byte = iota + 24 // followed by 1-byte data-item
	info25                  // followed by 2-byte data-item
	info26                  // followed by 4-byte data-item
	info27                  // followed by 8-byte data-item
	// 28..30 reserved
	indefiniteLength = 31 // for byte-string, string, arr, map
)

const ( // simple types for type7
	// 0..19 unassigned
	simpleTypeFalse byte = iota + 20 // encodes nil type
	simpleTypeTrue
	simpleTypeNil
	simpleUndefined
	simpleTypeByte // the actual type in next byte 32..255
	flt16          // IEEE 754 Half-Precision Float
	flt32          // IEEE 754 Single-Precision Float
	flt64          // IEEE 754 Double-Precision Float
	// 28..30 reserved
	itemBreak = 31 // stop-code for indefinite-length items
)

func major(b byte) byte {
	return b & 0xe0
}

func info(b byte) byte {
	return b & 0x1f
}

func hdr(major, info byte) byte {
	return (major & 0xe0) | (info & 0x1f)
}

//---- encode functions
//
//  * all encode functions shall optionally take an input value to encode, and
//   o/p byte-slice to save the o/p.
//  * all encode functions shall return the number of bytes encoded into the
//   o/p byte-slice.

func encodeNull(buf []byte) int {
	buf[0] = hdr(type7, simpleTypeNil)
	return 1
}

func encodeTrue(buf []byte) int {
	buf[0] = hdr(type7, simpleTypeTrue)
	return 1
}

func encodeFalse(buf []byte) int {
	buf[0] = hdr(type7, simpleTypeFalse)
	return 1
}

func encodeUint8(item byte, buf []byte) int {
	if item <= MaxSmallInt {
		buf[0] = hdr(type0, item) // 0..23
		return 1
	}
	buf[0] = hdr(type0, info24)
	buf[1] = item // 24..255
	return 2
}

func encodeInt8(item int8, buf []byte) int {
	if item > MaxSmallInt {
		buf[0] = hdr(type0, info24)
		buf[1] = byte(item) // 24..127
		return 2
	} else if item < -MaxSmallInt {
		buf[0] = hdr(type1, info24)
		buf[1] = byte(-(item + 1)) // -128..-24
		return 2
	} else if item < 0 {
		buf[0] = hdr(type1, byte(-(item + 1))) // -23..-1
		return 1
	}
	buf[0] = hdr(type0, byte(item)) // 0..23
	return 1
}

func encodeUint16(item uint16, buf []byte) int {
	if item < 256 {
		return encodeUint8(byte(item), buf)
	}
	buf[0] = hdr(type0, info25)
	binary.BigEndian.PutUint16(buf[1:], item) // 256..65535
	return 3
}

func encodeInt16(item int16, buf []byte) int {
	if item > 127 {
		if item < 256 {
			buf[0] = hdr(type0, info24)
			buf[1] = byte(item) // 128..255
			return 2
		}
		buf[0] = hdr(type0, info25)
		binary.BigEndian.PutUint16(buf[1:], uint16(item)) // 256..32767
		return 3

	} else if item < -128 {
		if item > -256 {
			buf[0] = hdr(type1, info24)
			buf[1] = byte(-(item + 1)) // -255..-129
			return 2
		}
		buf[0] = hdr(type1, info25) // -32768..-256
		binary.BigEndian.PutUint16(buf[1:], uint16(-(item + 1)))
		return 3
	}
	return encodeInt8(int8(item), buf)
}

func encodeUint32(item uint32, buf []byte) int {
	if item < 65536 {
		return encodeUint16(uint16(item), buf) // 0..65535
	}
	buf[0] = hdr(type0, info26)
	binary.BigEndian.PutUint32(buf[1:], item) // 65536 to 4294967295
	return 5
}

func encodeInt32(item int32, buf []byte) int {
	if item > 32767 {
		if item < 65536 {
			buf[0] = hdr(type0, info25)
			binary.BigEndian.PutUint16(buf[1:], uint16(item)) // 32768..65535
			return 3
		}
		buf[0] = hdr(type0, info26) // 65536 to 2147483647
		binary.BigEndian.PutUint32(buf[1:], uint32(item))
		return 5

	} else if item < -32768 {
		if item > -65536 {
			buf[0] = hdr(type1, info25) // -65535..-32769
			binary.BigEndian.PutUint16(buf[1:], uint16(-(item + 1)))
			return 3
		}
		buf[0] = hdr(type1, info26) // -2147483648..-65536
		binary.BigEndian.PutUint32(buf[1:], uint32(-(item + 1)))
		return 5
	}
	return encodeInt16(int16(item), buf)
}

func encodeUint64(item uint64, buf []byte) int {
	if item < 4294967296 {
		return encodeUint32(uint32(item), buf) // 0..4294967295
	}
	buf[0] = hdr(type0, info27) // 4294967296 to 18446744073709551615
	binary.BigEndian.PutUint64(buf[1:], item)
	return 9
}

func encodeInt64(item int64, buf []byte) int {
	if item > 2147483647 {
		if item < 4294967296 {
			buf[0] = hdr(type0, info26) // 2147483647..4294967296
			binary.BigEndian.PutUint32(buf[1:], uint32(item))
			return 5
		}
		buf[0] = hdr(type0, info27) // 4294967296..9223372036854775807
		binary.BigEndian.PutUint64(buf[1:], uint64(item))
		return 9

	} else if item < -2147483648 {
		if item > -4294967296 {
			buf[0] = hdr(type1, info26) // -4294967295..-2147483649
			binary.BigEndian.PutUint32(buf[1:], uint32(-(item + 1)))
			return 5
		}
		buf[0] = hdr(type1, info27) // -9223372036854775808..-4294967296
		binary.BigEndian.PutUint64(buf[1:], uint64(-(item + 1)))
		return 9
	}
	return encodeInt32(int32(item), buf)
}

func encodeFloat32(item float32, buf []byte) int {
	buf[0] = hdr(type7, flt32)
	binary.BigEndian.PutUint32(buf[1:], math.Float32bits(item))
	return 5
}

func encodeFloat64(item float64, buf []byte) int {
	buf[0] = hdr(type7, flt64)
	binary.BigEndian.PutUint64(buf[1:], math.Float64bits(item))
	return 9
}

func encodeBytes(item []byte, buf []byte) int {
	n := encodeUint64(uint64(len(item)), buf)
	buf[0] = (buf[0] & 0x1f) | type2 // fix the type from type0->type2
	copy(buf[n:], item)
	return n + len(item)
}

func encodeBytesStart(buf []byte) int {
	// indefinite chunks of byte string
	buf[0] = hdr(type2, byte(indefiniteLength))
	return 1
}

func encodeText(item string, buf []byte) int {
	n := encodeBytes(str2bytes(item), buf)
	buf[0] = (buf[0] & 0x1f) | type3 // fix the type from type2->type3
	return n
}

func encodeTextStart(buf []byte) int {
	buf[0] = hdr(type3, byte(indefiniteLength)) // indefinite chunks of text
	return 1
}

func encodeArray(items []interface{}, buf []byte) int {
	n := encodeUint64(uint64(len(items)), buf)
	buf[0] = (buf[0] & 0x1f) | type4 // fix the type from type0->type4
	n += encodeArrayItems(items, buf[n:])
	return n
}

func encodeArrayItems(items []interface{}, buf []byte) int {
	n := 0
	for _, item := range items {
		n += encode(item, buf[n:])
	}
	return n
}

func encodeArrayStart(buf []byte) int {
	buf[0] = hdr(type4, byte(indefiniteLength)) // indefinite length array
	return 1
}

func encodeMap(items [][2]interface{}, buf []byte) int {
	n := encodeUint64(uint64(len(items)), buf)
	buf[0] = (buf[0] & 0x1f) | type5 // fix the type from type0->type5
	n += encodeMapItems(items, buf[n:])
	return n
}

func encodeMapItems(items [][2]interface{}, buf []byte) int {
	n := 0
	for _, item := range items {
		n += encode(item[0], buf[n:])
		n += encode(item[1], buf[n:])
	}
	return n
}

func encodeMapStart(buf []byte) int {
	buf[0] = hdr(type5, byte(indefiniteLength)) // indefinite length map
	return 1
}

func encodeBreakStop(buf []byte) int {
	// break stop for indefinite array or map
	buf[0] = hdr(type7, byte(itemBreak))
	return 1
}

func encodeUndefined(buf []byte) int {
	buf[0] = hdr(type7, simpleUndefined)
	return 1
}

func encodeSimpleType(typcode byte, buf []byte) int {
	if typcode < 32 {
		buf[0] = hdr(type7, typcode)
		return 1
	}
	buf[0] = hdr(type7, simpleTypeByte)
	buf[1] = typcode
	return 2
}

func encode(item interface{}, out []byte) int {
	n := 0
	switch v := item.(type) {
	case nil:
		n += encodeNull(out)
	case bool:
		if v {
			n += encodeTrue(out)
		} else {
			n += encodeFalse(out)
		}
	case int8:
		n += encodeInt8(v, out)
	case uint8:
		n += encodeUint8(v, out)
	case int16:
		n += encodeInt16(v, out)
	case uint16:
		n += encodeUint16(v, out)
	case int32:
		n += encodeInt32(v, out)
	case uint32:
		n += encodeUint32(v, out)
	case int:
		n += encodeInt64(int64(v), out)
	case int64:
		n += encodeInt64(v, out)
	case uint:
		n += encodeUint64(uint64(v), out)
	case uint64:
		n += encodeUint64(v, out)
	case float32:
		n += encodeFloat32(v, out)
	case float64:
		n += encodeFloat64(v, out)
	case []byte:
		n += encodeBytes(v, out)
	case string:
		n += encodeText(v, out)
	case []interface{}:
		n += encodeArray(v, out)
	case [][2]interface{}:
		n += encodeMap(v, out)
	// simple types
	case Undefined:
		n += encodeUndefined(out)
	// tagged encoding
	case time.Time: // tag-0
		n += encodeDateTime(v, out)
	case Epoch: // tag-1
		n += encodeDateTime(v, out)
	case EpochMicro: // tag-1
		n += encodeDateTime(v, out)
	case *big.Int:
		n += encodeBigNum(v, out)
	case DecimalFraction:
		n += encodeDecimalFraction(v, out)
	case BigFloat:
		n += encodeBigFloat(v, out)
	case Cbor:
		n += encodeCbor(v, out)
	case *regexp.Regexp:
		n += encodeRegexp(v, out)
	case CborPrefix:
		n += encodeCborPrefix(v, out)
	default:
		panic(ErrorUnknownType)
	}
	return n
}

//---- decode functions

func decodeNull(buf []byte) (interface{}, int) {
	return nil, 1
}

func decodeFalse(buf []byte) (interface{}, int) {
	return false, 1
}

func decodeTrue(buf []byte) (interface{}, int) {
	return true, 1
}

func decodeSimpleTypeByte(buf []byte) (interface{}, int) {
	return buf[1], 2
}

func decodeFloat16(buf []byte) (interface{}, int) {
	panic(ErrorDecodeFloat16)
}

func decodeFloat32(buf []byte) (interface{}, int) {
	item, n := binary.BigEndian.Uint32(buf[1:]), 5
	return math.Float32frombits(item), n
}

func decodeFloat64(buf []byte) (interface{}, int) {
	item, n := binary.BigEndian.Uint64(buf[1:]), 9
	return math.Float64frombits(item), n
}

func decodeType0SmallInt(buf []byte) (interface{}, int) {
	return uint64(info(buf[0])), 1
}

func decodeType1SmallInt(buf []byte) (interface{}, int) {
	return -int64(info(buf[0]) + 1), 1
}

func decodeType0Info24(buf []byte) (interface{}, int) {
	return uint64(buf[1]), 2
}

func decodeType1Info24(buf []byte) (interface{}, int) {
	return -int64(buf[1] + 1), 2
}

func decodeType0Info25(buf []byte) (interface{}, int) {
	return uint64(binary.BigEndian.Uint16(buf[1:])), 3
}

func decodeType1Info25(buf []byte) (interface{}, int) {
	return -int64(binary.BigEndian.Uint16(buf[1:]) + 1), 3
}

func decodeType0Info26(buf []byte) (interface{}, int) {
	return uint64(binary.BigEndian.Uint32(buf[1:])), 5
}

func decodeType1Info26(buf []byte) (interface{}, int) {
	return -int64(binary.BigEndian.Uint32(buf[1:]) + 1), 5
}

func decodeType0Info27(buf []byte) (interface{}, int) {
	return uint64(binary.BigEndian.Uint64(buf[1:])), 9
}

func decodeType1Info27(buf []byte) (interface{}, int) {
	x := uint64(binary.BigEndian.Uint64(buf[1:]))
	if x > 9223372036854775807 {
		panic(ErrorDecodeExceedInt64)
	}
	return int64(-x) - 1, 9
}

func decodeLength(buf []byte) (int, int) {
	if y := info(buf[0]); y < info24 {
		return int(y), 1
	} else if y == info24 {
		return int(buf[1]), 2
	} else if y == info25 {
		return int(binary.BigEndian.Uint16(buf[1:])), 3
	} else if y == info26 {
		return int(binary.BigEndian.Uint32(buf[1:])), 5
	}
	return int(binary.BigEndian.Uint64(buf[1:])), 9 // info27
}

func decodeType2(buf []byte) (interface{}, int) {
	ln, n := decodeLength(buf)
	dst := make([]byte, ln)
	copy(dst, buf[n:n+ln])
	return dst, n + ln
}

func decodeType2Indefinite(buf []byte) (interface{}, int) {
	return Indefinite(buf[0]), 1
}

func decodeType3(buf []byte) (interface{}, int) {
	ln, n := decodeLength(buf)
	dst := make([]byte, ln)
	copy(dst, buf[n:n+ln])
	return bytes2str(dst), n + ln
}

func decodeType3Indefinite(buf []byte) (interface{}, int) {
	return Indefinite(buf[0]), 1
}

func decodeType4(buf []byte) (interface{}, int) {
	ln, n := decodeLength(buf)
	arr := make([]interface{}, ln)
	for i := 0; i < ln; i++ {
		item, n1 := decode(buf[n:])
		arr[i], n = item, n+n1
	}
	return arr, n
}

func decodeType4Indefinite(buf []byte) (interface{}, int) {
	return Indefinite(buf[0]), 1
}

func decodeType5(buf []byte) (interface{}, int) {
	ln, n := decodeLength(buf)
	pairs := make([][2]interface{}, ln)
	for i := 0; i < ln; i++ {
		key, n1 := decode(buf[n:])
		value, n2 := decode(buf[n+n1:])
		pairs[i] = [2]interface{}{key, value}
		n = n + n1 + n2
	}
	return pairs, n
}

func decodeType5Indefinite(buf []byte) (interface{}, int) {
	return Indefinite(buf[0]), 1
}

func decodeBreakCode(buf []byte) (interface{}, int) {
	return BreakStop(buf[0]), 1
}

func decodeUndefined(buf []byte) (interface{}, int) {
	return Undefined(simpleUndefined), 1
}

func decode(buf []byte) (interface{}, int) {
	item, n := cborDecoders[buf[0]](buf)
	if _, ok := item.(Indefinite); ok {
		switch major(buf[0]) {
		case type4:
			arr := make([]interface{}, 0, 2)
			for buf[n] != brkstp {
				item, n1 := decode(buf[n:])
				arr = append(arr, item)
				n += n1
			}
			return arr, n + 1

		case type5:
			pairs := make([][2]interface{}, 0, 2)
			for buf[n] != brkstp {
				key, n1 := decode(buf[n:])
				value, n2 := decode(buf[n+n1:])
				pairs = append(pairs, [2]interface{}{key, value})
				n = n + n1 + n2
			}
			return pairs, n + 1
		}
	}
	return item, n
}

//---- decoders

var cborDecoders = make(map[byte]func([]byte) (interface{}, int))

func init() {
	makePanic := func(msg error) func([]byte) (interface{}, int) {
		return func(_ []byte) (interface{}, int) { panic(msg) }
	}
	//-- type0                  (unsigned integer)
	// 1st-byte 0..23
	for i := byte(0); i < info24; i++ {
		cborDecoders[hdr(type0, i)] = decodeType0SmallInt
	}
	// 1st-byte 24..27
	cborDecoders[hdr(type0, info24)] = decodeType0Info24
	cborDecoders[hdr(type0, info25)] = decodeType0Info25
	cborDecoders[hdr(type0, info26)] = decodeType0Info26
	cborDecoders[hdr(type0, info27)] = decodeType0Info27
	// 1st-byte 28..31
	cborDecoders[hdr(type0, 28)] = makePanic(ErrorDecodeInfoReserved)
	cborDecoders[hdr(type0, 29)] = makePanic(ErrorDecodeInfoReserved)
	cborDecoders[hdr(type0, 30)] = makePanic(ErrorDecodeInfoReserved)
	cborDecoders[hdr(type0, indefiniteLength)] = makePanic(ErrorDecodeIndefinite)

	//-- type1                  (signed integer)
	// 1st-byte 0..23
	for i := byte(0); i < info24; i++ {
		cborDecoders[hdr(type1, i)] = decodeType1SmallInt
	}
	// 1st-byte 24..27
	cborDecoders[hdr(type1, info24)] = decodeType1Info24
	cborDecoders[hdr(type1, info25)] = decodeType1Info25
	cborDecoders[hdr(type1, info26)] = decodeType1Info26
	cborDecoders[hdr(type1, info27)] = decodeType1Info27
	// 1st-byte 28..31
	cborDecoders[hdr(type1, 28)] = makePanic(ErrorDecodeInfoReserved)
	cborDecoders[hdr(type1, 29)] = makePanic(ErrorDecodeInfoReserved)
	cborDecoders[hdr(type1, 30)] = makePanic(ErrorDecodeInfoReserved)
	cborDecoders[hdr(type1, indefiniteLength)] = makePanic(ErrorDecodeIndefinite)

	//-- type2                  (byte string)
	// 1st-byte 0..27
	for i := 0; i < 28; i++ {
		cborDecoders[hdr(type2, byte(i))] = decodeType2
	}
	// 1st-byte 28..31
	cborDecoders[hdr(type2, 28)] = makePanic(ErrorDecodeInfoReserved)
	cborDecoders[hdr(type2, 29)] = makePanic(ErrorDecodeInfoReserved)
	cborDecoders[hdr(type2, 30)] = makePanic(ErrorDecodeInfoReserved)
	cborDecoders[hdr(type2, indefiniteLength)] = decodeType2Indefinite

	//-- type3                  (string)
	// 1st-byte 0..27
	for i := 0; i < 28; i++ {
		cborDecoders[hdr(type3, byte(i))] = decodeType3
	}
	// 1st-byte 28..31
	cborDecoders[hdr(type3, 28)] = makePanic(ErrorDecodeInfoReserved)
	cborDecoders[hdr(type3, 29)] = makePanic(ErrorDecodeInfoReserved)
	cborDecoders[hdr(type3, 30)] = makePanic(ErrorDecodeInfoReserved)
	cborDecoders[hdr(type3, indefiniteLength)] = decodeType3Indefinite

	//-- type4                  (array)
	// 1st-byte 0..27
	for i := 0; i < 28; i++ {
		cborDecoders[hdr(type4, byte(i))] = decodeType4
	}
	// 1st-byte 28..31
	cborDecoders[hdr(type4, 28)] = makePanic(ErrorDecodeInfoReserved)
	cborDecoders[hdr(type4, 29)] = makePanic(ErrorDecodeInfoReserved)
	cborDecoders[hdr(type4, 30)] = makePanic(ErrorDecodeInfoReserved)
	cborDecoders[hdr(type4, indefiniteLength)] = decodeType4Indefinite

	//-- type5                  (map)
	// 1st-byte 0..27
	for i := 0; i < 28; i++ {
		cborDecoders[hdr(type5, byte(i))] = decodeType5
	}
	// 1st-byte 28..31
	cborDecoders[hdr(type5, 28)] = makePanic(ErrorDecodeInfoReserved)
	cborDecoders[hdr(type5, 29)] = makePanic(ErrorDecodeInfoReserved)
	cborDecoders[hdr(type5, 30)] = makePanic(ErrorDecodeInfoReserved)
	cborDecoders[hdr(type5, indefiniteLength)] = decodeType5Indefinite

	//-- type6
	// 1st-byte 0..23
	for i := byte(0); i < info24; i++ {
		cborDecoders[hdr(type6, i)] = decodeTag
	}
	// 1st-byte 24..27
	cborDecoders[hdr(type6, info24)] = decodeTag
	cborDecoders[hdr(type6, info25)] = decodeTag
	cborDecoders[hdr(type6, info26)] = decodeTag
	cborDecoders[hdr(type6, info27)] = decodeTag
	// 1st-byte 28..31
	cborDecoders[hdr(type6, 28)] = makePanic(ErrorDecodeInfoReserved)
	cborDecoders[hdr(type6, 29)] = makePanic(ErrorDecodeInfoReserved)
	cborDecoders[hdr(type6, 30)] = makePanic(ErrorDecodeInfoReserved)
	cborDecoders[hdr(type6, indefiniteLength)] = makePanic(ErrorDecodeIndefinite)

	//-- type7                  (simple types / floats / break-stop)
	// 1st-byte 0..19
	for i := byte(0); i < 20; i++ {
		cborDecoders[hdr(type7, i)] =
			func(i byte) func([]byte) (interface{}, int) {
				return func(buf []byte) (interface{}, int) { return i, 1 }
			}(i)
	}
	// 1st-byte 20..23
	cborDecoders[hdr(type7, simpleTypeFalse)] = decodeFalse
	cborDecoders[hdr(type7, simpleTypeTrue)] = decodeTrue
	cborDecoders[hdr(type7, simpleTypeNil)] = decodeNull
	cborDecoders[hdr(type7, simpleUndefined)] = decodeUndefined

	cborDecoders[hdr(type7, simpleTypeByte)] = decodeSimpleTypeByte
	cborDecoders[hdr(type7, flt16)] = decodeFloat16
	cborDecoders[hdr(type7, flt32)] = decodeFloat32
	cborDecoders[hdr(type7, flt64)] = decodeFloat64
	// 1st-byte 28..31
	cborDecoders[hdr(type7, 28)] = makePanic(ErrorDecodeSimpleType)
	cborDecoders[hdr(type7, 29)] = makePanic(ErrorDecodeSimpleType)
	cborDecoders[hdr(type7, 30)] = makePanic(ErrorDecodeSimpleType)
	cborDecoders[hdr(type7, itemBreak)] = decodeBreakCode
}
