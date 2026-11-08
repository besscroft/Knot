package hls

import (
	"fmt"
	"io"
	"os"

	"github.com/yapingcat/gomedia/go-mp4"
	"github.com/yapingcat/gomedia/go-mpeg2"
)

const (
	tsPacketSize = 188 // MPEG-TS 固定包长
	// tsSyncByte 是 TS 包同步字，也用于校验解密结果与缓存分片有效性。
	tsSyncByte = 0x47
	// goodRatioLimit 是成功转封装的有效数据占比下限（相对整个 TS 文件）。
	// TS 包封装开销（包头/PES 头/填充）约占 10-20%，低于此说明文件大部分是
	// 垃圾数据（如分片未解密、源损坏），继续产出残缺 MP4 没有意义。
	goodRatioLimit = 0.6
)

// Remux 把 MPEG-TS 转封装为 MP4（流式拷贝，不重编码）。
// 支持 H.264/H.265 视频与 AAC/MP3 音频；输出先写临时文件再原子改名。
//
// gomedia 解析到畸形数据会 panic 而非返回错误，这里整文件喂入（保证
// demuxer 按 PES 正确聚合帧），panic 时只回退跳过可疑包并继续喂入，
// 局部损坏仅损失对应片段；有效数据占比低于 goodRatioLimit 时报错，请重新下载。
func Remux(tsPath, mp4Path string) (err error) {
	in, err := os.Open(tsPath)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(mp4Path+".tmp", os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("无法创建 MP4 文件: %w", err)
	}
	// 失败（含 panic）时关句柄并清理临时文件；成功路径在 rename 前已显式 Close。
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("转封装异常: %v", r)
		}
		if err != nil {
			_ = out.Close()
			_ = os.Remove(mp4Path + ".tmp")
		}
	}()

	muxer, err := mp4.CreateMp4Muxer(out)
	if err != nil {
		return fmt.Errorf("初始化 MP4 封装器失败: %w", err)
	}

	var goodBytes int64
	var videoTrack, audioTrack uint32
	var hasVideo, hasAudio bool
	// 容错写出：gomedia 对个别畸形帧（如 AAC 伪同步字）解析会 panic，
	// 逐帧 recover 跳过，保证 panic 不向 demuxer 穿透（否则其内部缓冲状态会被打断）。
	// 成功写出的帧计入 goodBytes，用于整体有效性判定。
	write := func(track uint32, frame []byte, pts, dts uint64) {
		defer func() {
			_ = recover()
		}()
		if muxer.Write(track, frame, pts, dts) == nil {
			goodBytes += int64(len(frame))
		}
	}
	onFrame := func(cid mpeg2.TS_STREAM_TYPE, frame []byte, pts, dts uint64) {
		var track uint32
		switch cid {
		case mpeg2.TS_STREAM_H264:
			if !hasVideo {
				videoTrack = muxer.AddVideoTrack(mp4.MP4_CODEC_H264)
				hasVideo = true
			}
			track = videoTrack
		case mpeg2.TS_STREAM_H265:
			if !hasVideo {
				videoTrack = muxer.AddVideoTrack(mp4.MP4_CODEC_H265)
				hasVideo = true
			}
			track = videoTrack
		case mpeg2.TS_STREAM_AAC:
			if !hasAudio {
				audioTrack = muxer.AddAudioTrack(mp4.MP4_CODEC_AAC)
				hasAudio = true
			}
			// gomedia 内部的 ADTS 切分没有边界检查，遇到伪同步字/截断帧会越界；
			// 这里先自行校验并逐帧传入，单帧切片之后不再扫描后续内容。
			splitADTS(frame, func(one []byte) { write(audioTrack, one, pts, dts) })
			return
		case mpeg2.TS_STREAM_AUDIO_MPEG1, mpeg2.TS_STREAM_AUDIO_MPEG2:
			if !hasAudio {
				audioTrack = muxer.AddAudioTrack(mp4.MP4_CODEC_MP3)
				hasAudio = true
			}
			track = audioTrack
		default:
			return
		}
		write(track, frame, pts, dts)
	}

	total := info.Size()
	// feedAll 整段喂入 demuxer。正常流一次喂完；Input 对畸形包 panic/报错时
	// 跳过可疑的包（含 probe 的两包前瞻）后接着喂，demuxer 复用（PMT 等状态保留）。
	var pos int64
	dm := mpeg2.NewTSDemuxer()
	dm.OnFrame = onFrame
	for pos < total {
		if _, serr := in.Seek(pos, io.SeekStart); serr != nil {
			return serr
		}
		cr := &countReader{r: io.LimitReader(in, total-pos)}
		failed := false
		if ferr := func() (e error) {
			defer func() {
				if r := recover(); r != nil {
					e = fmt.Errorf("panic: %v", r)
					failed = true
				}
			}()
			return dm.Input(cr)
		}(); ferr == nil && !failed {
			break // 正常喂完（EOF）
		}
		// 畸形数据：回退两包（panic 包之前的包可能已部分处理，重喂去重），
		// 但每次至少前进一个包，保证必然推进。
		next := pos + cr.n - 2*tsPacketSize
		if next < pos+tsPacketSize {
			next = pos + tsPacketSize
		}
		if next >= total {
			break
		}
		pos = next
	}

	if !hasVideo && !hasAudio {
		return fmt.Errorf("TS 中没有可识别的音视频轨")
	}
	// TS 包封装开销（包头/PES 头/填充）约占 10-20%，有效内容远低于此说明
	// 文件大部分是垃圾数据（如分片未解密、源损坏），产出残缺 MP4 没有意义。
	if ratio := float64(goodBytes) / float64(total); ratio < goodRatioLimit {
		return fmt.Errorf("TS 数据损坏（有效数据仅占 %.1f%%），请重新下载", ratio*100)
	}
	if err = muxer.WriteTrailer(); err != nil {
		return fmt.Errorf("写入 MP4 元数据失败: %w", err)
	}
	if err = out.Close(); err != nil {
		return err
	}
	if err = os.Rename(mp4Path+".tmp", mp4Path); err != nil {
		return err
	}
	return nil
}

// countReader 统计已被读取的字节数，用于 panic 后定位继续位置。
type countReader struct {
	r io.Reader
	n int64
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// splitADTS 按 ADTS 头逐帧切分（带完整边界校验，容错伪同步字）。
// 帧（含 ADTS 头）原样回调；数据不以同步字开头或头部不完整时直接结束。
func splitADTS(data []byte, onFrame func(frame []byte)) {
	for {
		if len(data) < 7 || data[0] != 0xFF || data[1]&0xF0 != 0xF0 {
			return
		}
		hdr := 7
		if data[1]&0x01 == 0 {
			hdr = 9 // 有 CRC
		}
		length := int(data[3]&0x03)<<11 | int(data[4])<<3 | int(data[5])>>5
		if length < hdr || len(data) < length {
			return // 坏帧或截断，丢弃剩余
		}
		onFrame(data[:length])
		data = data[length:]
	}
}
