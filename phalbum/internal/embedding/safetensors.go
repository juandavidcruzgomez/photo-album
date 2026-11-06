package embedding

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
)

// readSafetensor loads one F32 tensor from a safetensors file: an 8-byte
// little-endian header length, a JSON header, then the raw tensor data.
func readSafetensor(path, name string) (data []float32, shape []int, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	if len(raw) < 8 {
		return nil, nil, fmt.Errorf("%s: too short", path)
	}
	headerLen := binary.LittleEndian.Uint64(raw)
	if headerLen > uint64(len(raw)-8) {
		return nil, nil, fmt.Errorf("%s: invalid header length", path)
	}
	var header map[string]json.RawMessage
	if err := json.Unmarshal(raw[8:8+headerLen], &header); err != nil {
		return nil, nil, fmt.Errorf("%s: parsing header: %w", path, err)
	}
	entry, ok := header[name]
	if !ok {
		return nil, nil, fmt.Errorf("%s: no tensor %q", path, name)
	}
	var info struct {
		DType   string `json:"dtype"`
		Shape   []int  `json:"shape"`
		Offsets [2]int `json:"data_offsets"`
	}
	if err := json.Unmarshal(entry, &info); err != nil {
		return nil, nil, fmt.Errorf("%s: parsing %q: %w", path, name, err)
	}
	if info.DType != "F32" {
		return nil, nil, fmt.Errorf("%s: %q is %s, want F32", path, name, info.DType)
	}
	n := 1
	for _, d := range info.Shape {
		n *= d
	}
	body := raw[8+headerLen:]
	start, end := info.Offsets[0], info.Offsets[1]
	if start < 0 || end > len(body) || end-start != 4*n {
		return nil, nil, fmt.Errorf("%s: %q has invalid data offsets", path, name)
	}
	data = make([]float32, n)
	for i := range data {
		data[i] = math.Float32frombits(binary.LittleEndian.Uint32(body[start+4*i:]))
	}
	return data, info.Shape, nil
}
