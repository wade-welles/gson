// Package also implements RFC-6901, JSON pointers.
// Pointers themself can be encoded into cbor format and
// vice-versa.
//
//   cbor-path :    | Text-chunk-start |
//                        | tagJsonString | len | segment1 |
//                        | tagJsonString | len | segment2 |
//                        ...
//                  | Break-stop |
package cbor

import "strconv"
import "bytes"

const maxPartSize int = 1024

// FromJsonPointer converts json path in RFC-6901 into cbor format,
func FromJsonPointer(path []byte, out []byte) int {
	var part [maxPartSize]byte

	if len(path) > 0 && path[0] != '/' {
		panic(ErrorExpectedJsonPointer)
	}

	n, off := encodeTextStart(out), 0
	for i := 0; i < len(path); {
		if path[i] == '~' {
			if path[i+1] == '1' {
				part[off] = '/'
				off, i = off+1, i+2

			} else if path[i+1] == '0' {
				part[off] = '~'
				off, i = off+1, i+2
			}

		} else if path[i] == '/' {
			if off > 0 {
				n += encodeTag(uint64(tagJsonString), out[n:])
				n += encodeText(bytes2str(part[:off]), out[n:])
				off = 0
			}
			i++

		} else {
			part[off] = path[i]
			i, off = i+1, off+1
		}
	}
	if off > 0 || (len(path) > 0 && path[len(path)-1] == '/') {
		n += encodeTag(uint64(tagJsonString), out[n:])
		n += encodeText(bytes2str(part[:off]), out[n:])
	}

	n += encodeBreakStop(out[n:])
	return n
}

// ToJsonPointer coverts cbor encoded path into json path RFC-6901
func ToJsonPointer(bin []byte, out []byte) int {
	if !IsIndefiniteText(Indefinite(bin[0])) {
		panic(ErrorExpectedCborPointer)
	}

	i, n, brkstp := 1, 0, hdr(type7, itemBreak)
	for {
		if bin[i] == hdr(type6, info24) && bin[i+1] == tagJsonString {
			i, out[n] = i+2, '/'
			n += 1
			ln, j := decodeLength(bin[i:])
			ln, i = ln+i+j, i+j
			for i < ln {
				switch bin[i] {
				case '/':
					out[n], out[n+1] = '~', '1'
					n += 2
				case '~':
					out[n], out[n+1] = '~', '0'
					n += 2
				default:
					out[n] = bin[i]
					n += 1
				}
				i++
			}
		}
		if bin[i] == brkstp {
			break
		}
	}
	return n
}

func partial(part, doc []byte) (start, end int) {
	if doc[0] == hdr(type4, byte(indefiniteLength)) {
		var index int
		var err error
		if part[0] == '-' {
			index = -1
		} else if index, err = strconv.Atoi(bytes2str(part)); err != nil {
			panic(ErrorInvalidArrayOffset)
		}
		n := arrayIndex(doc[1:], index)
		m := itemsEnd(doc[n:])
		return n, n + m

	} else if doc[0] == hdr(type5, byte(indefiniteLength)) {
		n := mapIndex(doc[1:], part)
		m := itemsEnd(doc[n:])   // key
		p := itemsEnd(doc[n+m:]) // value
		return n + m, n + m + p
	}
	panic(ErrorInvalidPointer)
}

func lookup(pointer, doc []byte) (start, end int) {
	if !IsIndefiniteText(Indefinite(pointer[0])) {
		panic(ErrorExpectedCborPointer)
	}

	i, brkstp := 1, hdr(type7, itemBreak)
	n, m := 0, len(doc)
	if pointer[i] == brkstp { // pointer is empty ""
		return n, m
	}
	for i < len(pointer) && pointer[i] != brkstp {
		doc = doc[n:m]
		if pointer[i] == hdr(type6, info24) && pointer[i+1] == tagJsonString {
			i += 2
			ln, j := decodeLength(pointer[i:])
			n, m = partial(pointer[i+j:i+j+ln], doc)
			i += j + ln
			continue
		}
		panic(ErrorInvalidPointer)
	}
	return n, m
}

func arrayIndex(arr []byte, index int) int {
	count, n, brkstp := 0, 0, hdr(type7, itemBreak)
	for index > 0 && count < index {
		m := itemsEnd(arr[n:])
		if index == -1 && arr[m] == brkstp {
			return n
		}
		n += m
		count++
	}
	return n
}

func mapIndex(buf []byte, part []byte) int {
	n, brkstp := 0, hdr(type7, itemBreak)
	for n < len(buf) {
		if buf[n] == brkstp {
			panic(ErrorNoKey)
		}
		m := itemsEnd(buf[n:]) // key
		if bytes.Compare(part, buf[n:m]) == 0 {
			return n
		}
		p := itemsEnd(buf[n+m:]) // value
		n += m + p
	}
	panic(ErrorMalformedDocument)
}

func itemsEnd(buf []byte) int {
	brkstp := hdr(type7, itemBreak)
	mjr, inf := major(buf[0]), info(buf[0])
	if mjr == type0 || mjr == type1 { // integer item
		if inf < info24 {
			return 1
		}
		return (1 << (inf - info24)) + 1

	} else if mjr == type3 { // string item
		ln, j := decodeLength(buf)
		return j + ln

	} else if mjr == type4 && info(buf[0]) == indefiniteLength { // array item
		n := 1 // skip indefiniteLength
		n += arrayIndex(buf[n:], -1)
		return n + itemsEnd(buf[n:]) + 1 // skip brkstp

	} else if mjr == type5 && info(buf[0]) == indefiniteLength { // map item
		n := 1 // skip indefiniteLength
		for n < len(buf) {
			if buf[n] == brkstp {
				return n + 1
			}
			n += itemsEnd(buf[n:]) // key
			n += itemsEnd(buf[n:]) // value
		}

	} else if mjr == type7 {
		if inf == simpleTypeNil || inf == simpleTypeFalse ||
			inf == simpleTypeTrue {
			return 1
		} else if inf == flt32 { // item float32
			return 1 + 4
		} else if inf == flt64 { // item float64
			return 1 + 8
		}
		panic(ErrorInvalidDocument)

	} else if mjr == type6 && buf[1] == tagJsonString && major(buf[2]) == type3 {
		ln, j := decodeLength(buf[2:])
		return 2 + j + ln
	}
	panic(ErrorInvalidDocument)
}

func get(doc, pointer []byte) []byte {
	n, m := lookup(pointer, doc)
	return doc[n:m]
}

func set(doc, pointer, item, out []byte) {
	n, m := lookup(pointer, doc)
	copy(out, doc[:n])
	copy(out, item)
	copy(out, doc[m:])
}

func del(doc, pointer, out []byte) {
	n, m := lookup(pointer, doc)
	copy(out, doc[:n])
	copy(out, doc[m:])
}