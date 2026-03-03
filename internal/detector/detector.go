package detector

import "errors"

type Detection struct {
	Label string     `json:"label"`
	Score float32    `json:"score"`
	Box   [4]float32 `json:"box"`
}

type Device string

const (
	DeviceCPU Device = "cpu"
	DeviceGPU Device = "gpu"
)

type Config struct {
	// ModelAssetPath is the embedded asset path to the ONNX file
	ModelAssetPath string

	// LabelsAssetPath is the embedded asset path to the labels file
	LabelsAssetPath string

	// InputSize is the modle input image size (square)
	InputSize int

	// ScoreThreshold filters low-confidence detections
	ScoreThreshold float32

	// NmsIouThreshold controls non-max suppression aggressiveness.
	// Only used if your postprocess includes NMS.
	NmsIouThreshold float32

	// MaxDetections caps detections returned after NMS/filtering.
	MaxDetections int

	// Device to choose the preffered loading RAM or VRAM
	Device Device // "cpu" or "gpu"
}

func DefaultConfig() Config {
	return Config{
		ModelAssetPath:  "assets/model.onnx",
		LabelsAssetPath: "assets/labels.txt",
		InputSize:       640,
		ScoreThreshold:  0.25,
		NmsIouThreshold: 0.45,
		MaxDetections:   100,
		Device:          DeviceCPU,
	}
}

type Detector interface {
	Detect(imageBytes []byte) ([]Detection, error)
	Close() error
}

func New(cfg Config) (Detector, error) {
	if cfg.ModelAssetPath == "" {
		return nil, errors.New("detector: ModelAssetPath is required")
	}
	if cfg.InputSize <= 0 {
		return nil, errors.New("detector: InputSize must be > 0")
	}
	if cfg.MaxDetections <= 0 {
		cfg.MaxDetections = 100
	}
	if cfg.ScoreThreshold <= 0 {
		cfg.ScoreThreshold = 0.25
	}
	if cfg.NmsIouThreshold <= 0 {
		cfg.NmsIouThreshold = 0.45
	}

	// newOnnxDetector is implemented in onnx.go
	return newOnnxDetector(cfg)
}

// NewWithInfo returns (detector, activeDevice, warning, error).
// activeDevice may be CPU even if cfg.Device is GPU (fallback).
func NewWithInfo(cfg Config) (Detector, Device, error, error) {
	d, err := newOnnxDetector(cfg)
	if err != nil {
		return nil, "", nil, err
	}
	od, ok := d.(*onnxDetector)
	if !ok {
		return d, cfg.Device, nil, nil
	}
	return d, od.activeDevice, od.warn, nil
}
