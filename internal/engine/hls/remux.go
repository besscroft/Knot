package hls

import (
	"fmt"
	"os"

	"github.com/yapingcat/gomedia/go-mp4"
	"github.com/yapingcat/gomedia/go-mpeg2"
)

// Remux 把 MPEG-TS 转封装为 MP4（流式拷贝，不重编码）。
// 支持 H.264/H.265 视频与 AAC/MP3 音频；输出先写临时文件再原子改名。
// gomedia 对个别畸形 AAC 帧会 panic，这里统一 recover 并回退由调用方处理。
func Remux(tsPath, mp4Path string) (err error) {
	in, err := os.Open(tsPath)
	if err != nil {
		return err
	}
	defer in.Close()

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

	var videoTrack, audioTrack uint32
	var hasVideo, hasAudio bool
	write := func(track uint32, frame []byte, pts, dts uint64) {
		_ = muxer.Write(track, frame, pts, dts)
	}
	demuxer := mpeg2.NewTSDemuxer()
	demuxer.OnFrame = func(cid mpeg2.TS_STREAM_TYPE, frame []byte, pts, dts uint64) {
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

	if err = demuxer.Input(in); err != nil {
		return fmt.Errorf("解析 TS 失败: %w", err)
	}
	if err = muxer.WriteTrailer(); err != nil {
		return fmt.Errorf("写入 MP4 元数据失败: %w", err)
	}
	if err = out.Close(); err != nil {
		return err
	}
	if !hasVideo && !hasAudio {
		_ = os.Remove(mp4Path + ".tmp")
		return fmt.Errorf("TS 中没有可识别的音视频轨")
	}
	if err = os.Rename(mp4Path+".tmp", mp4Path); err != nil {
		return err
	}
	return nil
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
