// Package video handles video file decoding via FFmpeg (cgo).
//
// Build requirements: CGO_ENABLED=1 with FFmpeg libraries available.
// On Windows, install FFmpeg development headers and link against:
//
//	avcodec, avformat, avutil, swscale
//
// pkg-config is the preferred way to locate the FFmpeg libraries:
//
//	#cgo pkg-config: libavcodec libavformat libavutil libswscale
package video

/*
#cgo pkg-config: libavcodec libavformat libavutil libswscale
#include <libavcodec/avcodec.h>
#include <libavformat/avformat.h>
#include <libavutil/imgutils.h>
#include <libavutil/opt.h>
#include <libswscale/swscale.h>
#include <stdlib.h>

// ffmpeg_open opens the media file at path and returns a populated DecodeCtx.
// Returns 0 on success, a negative AVERROR code on failure.
// The caller must free the returned context with ffmpeg_close().
typedef struct {
    AVFormatContext  *fmt_ctx;
    AVCodecContext   *codec_ctx;
    AVPacket         *packet;
    AVFrame          *frame;
    AVFrame          *sw_frame;
    struct SwsContext *sws_ctx;
    int               video_stream_idx;
    int               out_width;
    int               out_height;
    int               out_fmt;   // AV_PIX_FMT_RGBA or AV_PIX_FMT_NV12
} DecodeCtx;

static int ffmpeg_open(const char *path, int out_fmt, DecodeCtx **ctx_out) {
    DecodeCtx *ctx = (DecodeCtx *)calloc(1, sizeof(DecodeCtx));
    if (!ctx) return AVERROR(ENOMEM);

    int ret = avformat_open_input(&ctx->fmt_ctx, path, NULL, NULL);
    if (ret < 0) { free(ctx); return ret; }

    ret = avformat_find_stream_info(ctx->fmt_ctx, NULL);
    if (ret < 0) { avformat_close_input(&ctx->fmt_ctx); free(ctx); return ret; }

    ctx->video_stream_idx = av_find_best_stream(ctx->fmt_ctx, AVMEDIA_TYPE_VIDEO, -1, -1, NULL, 0);
    if (ctx->video_stream_idx < 0) {
        avformat_close_input(&ctx->fmt_ctx);
        free(ctx);
        return AVERROR_STREAM_NOT_FOUND;
    }

    AVStream *stream = ctx->fmt_ctx->streams[ctx->video_stream_idx];
    const AVCodec *codec = avcodec_find_decoder(stream->codecpar->codec_id);
    if (!codec) {
        avformat_close_input(&ctx->fmt_ctx);
        free(ctx);
        return AVERROR_DECODER_NOT_FOUND;
    }

    ctx->codec_ctx = avcodec_alloc_context3(codec);
    if (!ctx->codec_ctx) {
        avformat_close_input(&ctx->fmt_ctx);
        free(ctx);
        return AVERROR(ENOMEM);
    }

    ret = avcodec_parameters_to_context(ctx->codec_ctx, stream->codecpar);
    if (ret < 0) goto fail;

    ret = avcodec_open2(ctx->codec_ctx, codec, NULL);
    if (ret < 0) goto fail;

    ctx->out_width  = ctx->codec_ctx->width;
    ctx->out_height = ctx->codec_ctx->height;
    ctx->out_fmt    = out_fmt;

    ctx->sws_ctx = sws_getContext(
        ctx->codec_ctx->width, ctx->codec_ctx->height, ctx->codec_ctx->pix_fmt,
        ctx->out_width, ctx->out_height, (enum AVPixelFormat)out_fmt,
        SWS_BILINEAR, NULL, NULL, NULL);
    if (!ctx->sws_ctx) { ret = AVERROR(ENOMEM); goto fail; }

    ctx->packet   = av_packet_alloc();
    ctx->frame    = av_frame_alloc();
    ctx->sw_frame = av_frame_alloc();
    if (!ctx->packet || !ctx->frame || !ctx->sw_frame) { ret = AVERROR(ENOMEM); goto fail; }

    *ctx_out = ctx;
    return 0;

fail:
    avcodec_free_context(&ctx->codec_ctx);
    avformat_close_input(&ctx->fmt_ctx);
    av_packet_free(&ctx->packet);
    av_frame_free(&ctx->frame);
    av_frame_free(&ctx->sw_frame);
    if (ctx->sws_ctx) sws_freeContext(ctx->sws_ctx);
    free(ctx);
    return ret;
}

// ffmpeg_read_frame reads the next video frame into sw_frame.
// Returns 0 on success, AVERROR_EOF at end-of-stream, other negative on error.
static int ffmpeg_read_frame(DecodeCtx *ctx) {
    int ret;
    while (1) {
        ret = av_read_frame(ctx->fmt_ctx, ctx->packet);
        if (ret < 0) return ret; // AVERROR_EOF or real error

        if (ctx->packet->stream_index != ctx->video_stream_idx) {
            av_packet_unref(ctx->packet);
            continue;
        }

        ret = avcodec_send_packet(ctx->codec_ctx, ctx->packet);
        av_packet_unref(ctx->packet);
        if (ret < 0) return ret;

        ret = avcodec_receive_frame(ctx->codec_ctx, ctx->frame);
        if (ret == AVERROR(EAGAIN)) continue;
        if (ret < 0) return ret;

        // Allocate sw_frame buffer for converted output.
        ctx->sw_frame->format = ctx->out_fmt;
        ctx->sw_frame->width  = ctx->out_width;
        ctx->sw_frame->height = ctx->out_height;
        ret = av_image_alloc(ctx->sw_frame->data, ctx->sw_frame->linesize,
                             ctx->out_width, ctx->out_height,
                             (enum AVPixelFormat)ctx->out_fmt, 1);
        if (ret < 0) { av_frame_unref(ctx->frame); return ret; }

        sws_scale(ctx->sws_ctx,
                  (const uint8_t * const *)ctx->frame->data, ctx->frame->linesize,
                  0, ctx->codec_ctx->height,
                  ctx->sw_frame->data, ctx->sw_frame->linesize);

        av_frame_unref(ctx->frame);
        return 0;
    }
}

// ffmpeg_seek seeks the format context to the beginning of the stream.
static int ffmpeg_seek(DecodeCtx *ctx) {
    avcodec_flush_buffers(ctx->codec_ctx);
    return av_seek_frame(ctx->fmt_ctx, ctx->video_stream_idx, 0, AVSEEK_FLAG_BACKWARD);
}

// ffmpeg_frame_pts returns the PTS of the last decoded frame in microseconds.
static int64_t ffmpeg_frame_pts(DecodeCtx *ctx) {
    AVRational tb = ctx->fmt_ctx->streams[ctx->video_stream_idx]->time_base;
    return av_rescale_q(ctx->sw_frame->pts, tb, (AVRational){1, 1000000});
}

// ffmpeg_frame_stride returns the stride (linesize[0]) of the last decoded frame.
static int ffmpeg_frame_stride(DecodeCtx *ctx) {
    return ctx->sw_frame->linesize[0];
}

// ffmpeg_close releases all resources owned by ctx.
static void ffmpeg_close(DecodeCtx *ctx) {
    if (!ctx) return;
    av_freep(&ctx->sw_frame->data[0]);
    av_frame_free(&ctx->sw_frame);
    av_frame_free(&ctx->frame);
    av_packet_free(&ctx->packet);
    sws_freeContext(ctx->sws_ctx);
    avcodec_free_context(&ctx->codec_ctx);
    avformat_close_input(&ctx->fmt_ctx);
    free(ctx);
}
*/
import "C"

import (
	"fmt"
	"io"
	"time"
	"unsafe"
)

// Decoder decodes video frames from a file using FFmpeg.
type Decoder struct {
	ctx    *C.DecodeCtx
	format PixelFormat
}

// Open opens the video file at path and prepares it for frame-by-frame decoding.
// format selects the pixel layout of decoded frames (RGBA or NV12).
func Open(path string, format PixelFormat) (*Decoder, error) {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	var avFmt C.int
	switch format {
	case PixelFormatRGBA:
		avFmt = C.AV_PIX_FMT_RGBA
	case PixelFormatNV12:
		avFmt = C.AV_PIX_FMT_NV12
	default:
		return nil, fmt.Errorf("video: unsupported pixel format %d", format)
	}

	var ctx *C.DecodeCtx
	if ret := C.ffmpeg_open(cPath, avFmt, &ctx); ret < 0 {
		return nil, fmt.Errorf("video: ffmpeg_open failed (AVERROR %d)", int(ret))
	}
	return &Decoder{ctx: ctx, format: format}, nil
}

// ReadFrame decodes and returns the next video frame.
// Returns (nil, io.EOF) at end-of-stream so callers can loop.
func (d *Decoder) ReadFrame() (*Frame, error) {
	ret := C.ffmpeg_read_frame(d.ctx)
	if ret < 0 {
		if ret == C.AVERROR_EOF {
			return nil, io.EOF
		}
		return nil, fmt.Errorf("video: ffmpeg_read_frame failed (AVERROR %d)", int(ret))
	}

	w := int(d.ctx.out_width)
	h := int(d.ctx.out_height)
	stride := int(C.ffmpeg_frame_stride(d.ctx))
	ptsMicros := int64(C.ffmpeg_frame_pts(d.ctx))

	var dataSize int
	switch d.format {
	case PixelFormatRGBA:
		dataSize = stride * h
	case PixelFormatNV12:
		// Y plane (stride × h) + UV plane (stride × h/2)
		dataSize = stride*h + stride*(h/2)
	}

	// Copy pixel data out of the FFmpeg-managed buffer before the next decode call.
	data := make([]byte, dataSize)
	copy(data, (*[1 << 30]byte)(unsafe.Pointer(d.ctx.sw_frame.data[0]))[:dataSize:dataSize])

	// Release the temporary sw_frame data buffer allocated by ffmpeg_read_frame.
	C.av_freep(unsafe.Pointer(&d.ctx.sw_frame.data[0]))

	return &Frame{
		Width:  w,
		Height: h,
		Stride: stride,
		Data:   data,
		Format: d.format,
		PTS:    time.Duration(ptsMicros) * time.Microsecond,
	}, nil
}

// Seek rewinds the stream to the beginning for looped playback.
func (d *Decoder) Seek() error {
	if ret := C.ffmpeg_seek(d.ctx); ret < 0 {
		return fmt.Errorf("video: seek failed (AVERROR %d)", int(ret))
	}
	return nil
}

// Close releases all FFmpeg resources held by the Decoder.
func (d *Decoder) Close() {
	C.ffmpeg_close(d.ctx)
	d.ctx = nil
}

// Width returns the frame width in pixels.
func (d *Decoder) Width() int { return int(d.ctx.out_width) }

// Height returns the frame height in pixels.
func (d *Decoder) Height() int { return int(d.ctx.out_height) }
