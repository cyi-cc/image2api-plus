package util

import (
	"bytes"
	"encoding/binary"
	"math"
)

// maxMediaDurationSec 时长 sanity 上限：解析出超过 24h 视为垃圾数据丢弃。
const maxMediaDurationSec = 24 * 3600

// MediaDurationSeconds 从已读片段尽力解析媒体时长（秒）。
// mp4/mov/m4a 扫描 mvhd 盒子；webm 解 EBML Duration×TimecodeScale；wav 按
// byteRate 换算。解析不出返回 false——调用方可用文件尾片段再试一次
// （未 faststart 的 mp4 moov 盒子常在文件尾）。mp3/ogg 无廉价可靠头长度，跳过。
func MediaDurationSeconds(mime string, buf []byte) (float64, bool) {
	if len(buf) == 0 {
		return 0, false
	}
	var sec float64
	var ok bool
	switch mime {
	case "video/mp4", "video/quicktime", "audio/mp4":
		sec, ok = mvhdDuration(buf)
	case "video/webm":
		sec, ok = webmDuration(buf)
	case "audio/wav":
		sec, ok = wavDuration(buf)
	}
	if !ok || sec <= 0 || sec > maxMediaDurationSec {
		return 0, false
	}
	return sec, true
}

// mvhdDuration 在缓冲里扫 "mvhd" 盒子并解析 duration/timescale。
// 盒子布局：[size4][type4=mvhd][version1][flags3]
// v0: creation4 mod4 timescale4 duration4 → timescale@idx+16, duration@idx+20
// v1: creation8 mod8 timescale4 duration8 → timescale@idx+24, duration@idx+28
func mvhdDuration(buf []byte) (float64, bool) {
	for off := 0; ; {
		i := bytes.Index(buf[off:], []byte("mvhd"))
		if i < 0 {
			return 0, false
		}
		i += off
		if ver := buf[i+4]; ver == 0 && i+24 <= len(buf) {
			ts := binary.BigEndian.Uint32(buf[i+16 : i+20])
			dur := binary.BigEndian.Uint32(buf[i+20 : i+24])
			if ts > 0 && dur > 0 {
				return float64(dur) / float64(ts), true
			}
		} else if ver == 1 && i+36 <= len(buf) {
			ts := binary.BigEndian.Uint32(buf[i+24 : i+28])
			dur := binary.BigEndian.Uint64(buf[i+28 : i+36])
			if ts > 0 && dur > 0 {
				return float64(dur) / float64(ts), true
			}
		}
		off = i + 4
	}
}

// webmDuration 扫 EBML Duration 元素（0x4489，float）和 TimecodeScale
// （0x2AD7B1，uint，缺省 1,000,000ns）。秒 = duration × scale / 1e9。
func webmDuration(buf []byte) (float64, bool) {
	scale := uint64(1_000_000)
	if i := bytes.Index(buf, []byte{0x2a, 0xd7, 0xb1}); i >= 0 {
		if v, n := ebmlVint(buf[i+3:]); n > 0 && i+3+n+v <= len(buf) {
			scale = 0
			for _, b := range buf[i+3+n : i+3+n+v] {
				scale = scale<<8 | uint64(b)
			}
			if scale == 0 {
				scale = 1_000_000
			}
		}
	}
	i := bytes.Index(buf, []byte{0x44, 0x89})
	if i < 0 {
		return 0, false
	}
	v, n := ebmlVint(buf[i+2:])
	if n == 0 || i+2+n+v > len(buf) {
		return 0, false
	}
	f := buf[i+2+n : i+2+n+v]
	var dur float64
	switch v {
	case 4:
		dur = float64(math.Float32frombits(binary.BigEndian.Uint32(f)))
	case 8:
		dur = math.Float64frombits(binary.BigEndian.Uint64(f))
	default:
		return 0, false
	}
	return dur * float64(scale) / 1e9, true
}

// ebmlVint 读 EBML 变长整数的长度值：返回 (负载长度, vint 本身字节数)。
func ebmlVint(buf []byte) (int, int) {
	if len(buf) == 0 {
		return 0, 0
	}
	for n := 1; n <= 8; n++ {
		mask := byte(0x80 >> (n - 1))
		if buf[0]&mask != 0 {
			if len(buf) < n {
				return 0, 0
			}
			v := int(buf[0] &^ mask)
			for i := 1; i < n; i++ {
				v = v<<8 | int(buf[i])
			}
			return v, n
		}
	}
	return 0, 0
}

// wavDuration 解析 RIFF/WAVE：byteRate@28(LE u32) + "data" 块大小换算秒数。
func wavDuration(buf []byte) (float64, bool) {
	if len(buf) < 44 || string(buf[:4]) != "RIFF" || string(buf[8:12]) != "WAVE" {
		return 0, false
	}
	rate := binary.LittleEndian.Uint32(buf[28:32])
	if rate == 0 {
		return 0, false
	}
	for off := 12; off+8 <= len(buf); {
		size := int(binary.LittleEndian.Uint32(buf[off+4 : off+8]))
		if string(buf[off:off+4]) == "data" {
			return float64(size) / float64(rate), true
		}
		off += 8 + size + size&1 // 块按 2 字节对齐
	}
	return 0, false
}
