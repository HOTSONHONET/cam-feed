package detector

import (
	"bytes"
	"embed"
	"fmt"
	"log"
	"os"
	"sync"

	"image"
	_ "image/jpeg" // Import for JPEG support

	_ "image/png" // Import for PNG support

	ort "github.com/yalue/onnxruntime_go"
)

//go:embed assets/*
var assetsFS embed.FS

type onnxDetector struct {
	mu  sync.Mutex
	cfg Config

	activeDevice Device
	warn         error // e.g. "GPU init failed; using CPU"

	labels []string

	// Model file written from embedded FS (ORT session requires a filepath)
	modelPath string

	// ORT objects
	session      *ort.AdvancedSession
	inputTensor  *ort.Tensor[float32]
	outputTensor *ort.Tensor[float32]

	// Cached tensor backing arrays
	inputData  []float32
	outputData []float32

	// Input/output names for session run
	inName  string
	outName string
}

func newOnnxDetector(cfg Config) (Detector, error) {
	// 1) Acquire ORT env for specified device (with fallback)
	dev, warn, err := rt.acquire(cfg.Device)
	if err != nil {
		return nil, err
	}

	// 2) Load labels from embedded FS
	labels, err := readLabels(cfg.LabelsAssetPath)
	if err != nil {
		rt.release()
		return nil, err
	}

	// 3) Write embedded model to temp file
	modelPath, err := writeEmbeddedToTemp(cfg.ModelAssetPath, "model-*.onnx")
	if err != nil {
		rt.release()
		return nil, err
	}

	// 4) Define ONNX graph
	inName := "images"
	outName := "output0"

	// 5) Create input/output tensors (input -> [1, 3, 640, 640], output -> [1, 84, 8400])
	inputShape := ort.NewShape(1, 3, int64(cfg.InputSize), int64(cfg.InputSize))
	inputData := make([]float32, 1*3*cfg.InputSize*cfg.InputSize)
	inTensor, err := ort.NewTensor(inputShape, inputData)
	if err != nil {
		rt.release()
		_ = os.Remove(modelPath)
		return nil, err
	}

	outputShape := ort.NewShape(1, 84, 8400)
	outTensor, err := ort.NewEmptyTensor[float32](outputShape)
	if err != nil {
		inTensor.Destroy()
		rt.release()
		_ = os.Remove(modelPath)
		return nil, err
	}

	// 6) Create ORT session
	sess, err := ort.NewAdvancedSession(
		modelPath,
		[]string{inName},
		[]string{outName},
		[]ort.Value{inTensor},
		[]ort.Value{outTensor},
		nil,
	)

	if err != nil {
		outTensor.Destroy()
		inTensor.Destroy()
		rt.release()
		_ = os.Remove(modelPath)
		return nil, err
	}

	d := &onnxDetector{
		cfg:          cfg,
		activeDevice: dev,
		warn:         warn,
		labels:       labels,
		modelPath:    modelPath,
		session:      sess,
		inputTensor:  inTensor,
		outputTensor: outTensor,
		inputData:    inputData,
		outputData:   outTensor.GetData(),
		inName:       inName,
		outName:      outName,
	}

	return d, nil
}

func (d *onnxDetector) Detect(imageBytes []byte) ([]Detection, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Decode
	img, _, err := image.Decode(bytes.NewReader(imageBytes))
	if err != nil {
		return nil, err
	}

	meta, err := preprocessToNCHW(img, d.cfg.InputSize, d.inputData)
	if err != nil {
		return nil, err
	}

	if err := d.session.Run(); err != nil {
		return nil, err
	}

	log.Printf("[detector] session run completed: %v", d.outputData[:10]) // log first 10 elements

	dets := postprocessYOLOv8(d.outputData, meta, d.cfg, d.labels)

	if len(dets) > 0 {
		fmt.Printf("[detector] dets=%d first=%+v orig=%dx%d pad=(%d,%d) scale=%.4f\n",
			len(dets), dets[0], meta.OrigW, meta.OrigH, meta.PadX, meta.PadY, meta.Scale)
	} else {
		fmt.Printf("[detector] dets=0 (score_th=%.2f)\n", d.cfg.ScoreThreshold)
	}

	return dets, nil
}

func (d *onnxDetector) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.session != nil {
		d.session.Destroy()
		d.session = nil
	}

	if d.outputTensor != nil {
		d.outputTensor.Destroy()
		d.outputTensor = nil
	}

	if d.inputTensor != nil {
		d.inputTensor.Destroy()
		d.inputTensor = nil
	}

	if d.modelPath != "" {
		_ = os.Remove(d.modelPath)
		d.modelPath = ""
	}

	rt.release()
	return nil
}

func writeEmbeddedToTemp(assetPath, pattern string) (string, error) {
	data, err := assetsFS.ReadFile(assetPath)
	if err != nil {
		return "", err
	}

	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}

	defer f.Close()

	if _, err := f.Write(data); err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}

	return f.Name(), nil
}

func readLabels(assetPath string) ([]string, error) {
	data, err := assetsFS.ReadFile(assetPath)
	if err != nil {
		return nil, err
	}

	lines := bytes.Split(data, []byte{'\n'})
	var labels []string
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) > 0 {
			labels = append(labels, string(line))
		}
	}

	return labels, nil
}
