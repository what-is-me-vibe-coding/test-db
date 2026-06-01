package storage

import (
	"fmt"

	"github.com/klauspost/compress/zstd"
)

var encoder *zstd.Encoder
var decoder *zstd.Decoder

func init() {
	var err error
	encoder, err = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		panic(fmt.Sprintf("zstd encoder init: %v", err))
	}
	decoder, err = zstd.NewReader(nil)
	if err != nil {
		panic(fmt.Sprintf("zstd decoder init: %v", err))
	}
}

// Compress 使用 ZSTD 压缩数据，返回压缩后的字节切片。
func Compress(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}
	return encoder.EncodeAll(data, nil)
}

// Decompress 解压 ZSTD 压缩的数据，返回原始字节切片。
func Decompress(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	result, err := decoder.DecodeAll(data, nil)
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
	enc.Data = Compress(enc.Data)
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
