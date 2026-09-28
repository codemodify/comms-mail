package mailcore

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"
	"unicode/utf16"
)

// Binary property lists ("bplist00"), as macOS writes preferences and the
// values in its Accounts database, decoded into the same Go values as
// decodePlist gives for XML ones: map[string]any, []any, string, int64,
// float64, bool, []byte, time.Time — plus plistUID, the object references
// NSKeyedArchiver uses (unarchiveKeyed resolves them).

// plistUID is a CF$UID: an index into an archive's $objects.
type plistUID uint64

// appleEpoch is where plist dates count from (2001-01-01 UTC).
var appleEpoch = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

var errBadBplist = errors.New("mail: not a readable binary property list")

func isBinaryPlist(b []byte) bool { return bytes.HasPrefix(b, []byte("bplist00")) }

// decodeBinaryPlist reads a whole binary property list.
func decodeBinaryPlist(b []byte) (any, error) {
	if !isBinaryPlist(b) || len(b) < 8+32 {
		return nil, errBadBplist
	}
	t := b[len(b)-32:]
	p := &bplist{
		b:        b,
		offSize:  int(t[6]),
		refSize:  int(t[7]),
		nObjects: binary.BigEndian.Uint64(t[8:16]),
	}
	top := binary.BigEndian.Uint64(t[16:24])
	p.table = binary.BigEndian.Uint64(t[24:32])
	if p.offSize < 1 || p.offSize > 8 || p.refSize < 1 || p.refSize > 8 || top >= p.nObjects ||
		p.table >= uint64(len(b)) || p.nObjects > uint64(len(b)) ||
		p.table+p.nObjects*uint64(p.offSize) > uint64(len(b)-32) {
		return nil, errBadBplist
	}
	return p.object(top, 0)
}

type bplist struct {
	b                []byte
	offSize, refSize int
	nObjects, table  uint64
}

// uint reads an n-byte big-endian number at off.
func (p *bplist) uint(off uint64, n int) (uint64, bool) {
	if n < 1 || n > 8 || off+uint64(n) > uint64(len(p.b)) {
		return 0, false
	}
	var v uint64
	for _, c := range p.b[off : off+uint64(n)] {
		v = v<<8 | uint64(c)
	}
	return v, true
}

func (p *bplist) object(ref uint64, depth int) (any, error) {
	if ref >= p.nObjects || depth > 64 {
		return nil, errBadBplist
	}
	off, ok := p.uint(p.table+ref*uint64(p.offSize), p.offSize)
	if !ok || off < 8 || off >= uint64(len(p.b)) {
		return nil, errBadBplist
	}
	marker := p.b[off]
	kind, info := marker>>4, int(marker&0x0f)
	off++
	// count reads the length of a data, string or collection: in the low
	// nibble, or in the int that follows when it is 0xF.
	count := func() (uint64, bool) {
		if info != 0x0f {
			return uint64(info), true
		}
		if off >= uint64(len(p.b)) || p.b[off]>>4 != 0x1 {
			return 0, false
		}
		n := 1 << (p.b[off] & 0x0f)
		v, ok := p.uint(off+1, min(n, 8))
		off += 1 + uint64(n)
		return v, ok
	}
	switch kind {
	case 0x0:
		switch marker {
		case 0x08:
			return false, nil
		case 0x09:
			return true, nil
		}
		return nil, nil
	case 0x1:
		n := 1 << info
		if n > 16 {
			return nil, errBadBplist
		}
		// 16-byte ints hold a 64-bit value in their low half.
		v, ok := p.uint(off+uint64(max(n-8, 0)), min(n, 8))
		if !ok {
			return nil, errBadBplist
		}
		return int64(v), nil
	case 0x2:
		switch info {
		case 2:
			v, ok := p.uint(off, 4)
			if !ok {
				return nil, errBadBplist
			}
			return float64(math.Float32frombits(uint32(v))), nil
		case 3:
			v, ok := p.uint(off, 8)
			if !ok {
				return nil, errBadBplist
			}
			return math.Float64frombits(v), nil
		}
		return nil, errBadBplist
	case 0x3:
		v, ok := p.uint(off, 8)
		if !ok {
			return nil, errBadBplist
		}
		return plistTime(math.Float64frombits(v)), nil
	case 0x4, 0x5, 0x7:
		n, ok := count()
		if !ok || off+n > uint64(len(p.b)) {
			return nil, errBadBplist
		}
		if kind == 0x4 {
			return append([]byte(nil), p.b[off:off+n]...), nil
		}
		return string(p.b[off : off+n]), nil
	case 0x6:
		n, ok := count()
		if !ok || off+2*n > uint64(len(p.b)) {
			return nil, errBadBplist
		}
		u := make([]uint16, n)
		for i := range u {
			u[i] = binary.BigEndian.Uint16(p.b[off+2*uint64(i):])
		}
		return string(utf16.Decode(u)), nil
	case 0x8:
		v, ok := p.uint(off, info+1)
		if !ok {
			return nil, errBadBplist
		}
		return plistUID(v), nil
	case 0xa, 0xc, 0xd:
		n, ok := count()
		refs := n
		if kind == 0xd {
			refs = 2 * n
		}
		if !ok || n > p.nObjects || off+refs*uint64(p.refSize) > uint64(len(p.b)) {
			return nil, errBadBplist
		}
		ref := func(i uint64) uint64 {
			v, _ := p.uint(off+i*uint64(p.refSize), p.refSize)
			return v
		}
		if kind != 0xd {
			out := make([]any, 0, n)
			for i := uint64(0); i < n; i++ {
				v, err := p.object(ref(i), depth+1)
				if err != nil {
					return nil, err
				}
				out = append(out, v)
			}
			return out, nil
		}
		out := make(map[string]any, n)
		for i := uint64(0); i < n; i++ {
			k, err := p.object(ref(i), depth+1)
			if err != nil {
				return nil, err
			}
			v, err := p.object(ref(n+i), depth+1)
			if err != nil {
				return nil, err
			}
			out[fmt.Sprint(k)] = v
		}
		return out, nil
	}
	return nil, errBadBplist
}

func plistTime(secs float64) time.Time {
	if math.IsNaN(secs) || math.IsInf(secs, 0) || math.Abs(secs) > 1e11 {
		return time.Time{}
	}
	return appleEpoch.Add(time.Duration(secs * float64(time.Second)))
}

// unarchiveKeyed turns an NSKeyedArchiver archive (a decoded plist with
// $archiver, $objects and $top) into plain values: NSArray and NSSet into
// []any, NSDictionary into map[string]any, NSString, NSNumber, NSDate and
// NSData into string, int64/float64/bool, time.Time and []byte, and any
// other object into a map of its fields. ok is false when v is not one.
func unarchiveKeyed(v any) (any, bool) {
	root, _ := v.(map[string]any)
	if root == nil || asString(root["$archiver"]) != "NSKeyedArchiver" {
		return nil, false
	}
	objs, _ := root["$objects"].([]any)
	top, _ := root["$top"].(map[string]any)
	uid, ok := top["root"].(plistUID)
	if !ok {
		for _, x := range top { // an archive with one top object of another name
			if u, isUID := x.(plistUID); isUID && len(top) == 1 {
				uid, ok = u, true
			}
		}
	}
	if !ok {
		return nil, false
	}
	var resolve func(x any, depth int) any
	resolve = func(x any, depth int) any {
		if depth > 32 {
			return nil
		}
		u, isUID := x.(plistUID)
		if !isUID {
			return x
		}
		if u == 0 || u >= plistUID(len(objs)) {
			return nil // $null, or a reference past the end
		}
		o := objs[u]
		m, isMap := o.(map[string]any)
		if !isMap {
			if s, isStr := o.(string); isStr && s == "$null" {
				return nil
			}
			return o
		}
		if items, has := m["NS.objects"].([]any); has {
			if keys, isDict := m["NS.keys"].([]any); isDict {
				out := map[string]any{}
				for i, k := range keys {
					if i < len(items) {
						out[asString(resolve(k, depth+1))] = resolve(items[i], depth+1)
					}
				}
				return out
			}
			out := make([]any, 0, len(items))
			for _, it := range items {
				out = append(out, resolve(it, depth+1))
			}
			return out
		}
		if s, has := m["NS.string"]; has {
			return asString(resolve(s, depth+1))
		}
		if t, has := m["NS.time"].(float64); has {
			return plistTime(t)
		}
		for _, k := range []string{"NS.data", "NS.bytes"} {
			if b, has := m[k].([]byte); has {
				return b
			}
		}
		out := map[string]any{}
		for k, val := range m {
			if k != "$class" {
				out[k] = resolve(val, depth+1)
			}
		}
		return out
	}
	return resolve(uid, 0), true
}
