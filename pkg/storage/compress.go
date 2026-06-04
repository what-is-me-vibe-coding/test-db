package storage

import (
	"fmt"
	"sync"

	"github.com/klauspost/compress/zstd"
)

var (
	encoderOnce sync.Once
	encoderVal  *zstd.Encoder
	encoderErr  error

	decoderOnce sync.Once
	decoderVal  *zstd.Decoder
	decoderErr  error
)

// initEncoder 延迟初始化 ZSTD 编码器，避免在 init() 中使用 panic。
func initEncoder() (*zstd.Encoder, error) {
	encoderOnce.Do(func() {
		encoderVal, encoderErr = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	})
	return encoderVal, encoderErr
}

// initDecoder 延迟初始化 ZSTD 解码器，避免在 init() 中使用 panic。
func initDecoder() (*zstd.Decoder, error) {
	decoderOnce.Do(func() {
		decoderVal, decoderErr = zstd.NewReader(nil)
	})
	return decoderVal, decoderErr
}

// Compress 使用 ZSTD 压缩数据，返回压缩后的字节切片。
func Compress(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	enc, err := initEncoder()
	if err != nil {
		return nil, fmt.Errorf("zstd encoder init: %w", err)
	}
	return enc.EncodeAll(data, nil), nil
}

// Decompress 解压 ZSTD 压缩的数据，返回原始字节切片。
func Decompress(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	dec, err := initDecoder()
	if err != nil {
		return nil, fmt.Errorf("zstd decoder init: %w", err)
	}
	result, err := dec.DecodeAll(data, nil)
	if err != nil {
		return nil, fmt.Errorf("zstd decompress: %w", err)
	}
	return result, nil
}

// CompressColumn 压缩 EncodedColumn 中的编码数据。
func CompressColumn(enc *EncodedColumn) error {
	if enc == nil {
		return fmt.Errorf("compress column: nil EncodedColumn")
	}
	compressed, err := Compress(enc.Data)
	if err != nil {
		return fmt.Errorf("compress column: %w", err)
	}
	enc.Data = compressed
	return nil
}

// DecompressColumn 解压 EncodedColumn 中的压缩数据。
func DecompressColumn(enc *EncodedColumn) error {
	if enc == nil {
		return fmt.Errorf("decompress column: nil EncodedColumn")
	}
	data, err := Decompress(enc.Data)
	if err != nil {
		return err
	}
	enc.Data = data
	return nil
}
