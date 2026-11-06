package upload

import (
	"fmt"

	"github.com/klauspost/compress/zstd"
)

// compress zstd-compresses data at level 3 (tech stack: klauspost/compress/zstd).
func compress(data []byte) ([]byte, error) {
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(3)))
	if err != nil {
		return nil, fmt.Errorf("creating zstd encoder: %w", err)
	}
	defer func() { _ = enc.Close() }()
	return enc.EncodeAll(data, nil), nil
}
