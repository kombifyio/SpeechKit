//go:build cgo

package vad

import (
	"fmt"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

const (
	SampleRate     = 16000
	FrameSize      = 512 // 32ms at 16kHz
	BytesPerSample = 2
	FrameBytes     = FrameSize * BytesPerSample
	stateSize      = 2 * 1 * 128
)

// Detector is the speech-probability contract consumed by dictation processors.
type Detector interface {
	ProcessFrame([]int16) (float32, error)
	Reset()
}

// SileroVAD runs voice activity detection via ONNX Runtime.
// <1ms per frame, ~2MB model, no CGo beyond onnxruntime DLL.
type SileroVAD struct {
	session *ort.AdvancedSession

	inputTensor  *ort.Tensor[float32]
	srTensor     *ort.Tensor[int64]
	hTensor      *ort.Tensor[float32]
	cTensor      *ort.Tensor[float32]
	outputTensor *ort.Tensor[float32]
	hnTensor     *ort.Tensor[float32]
	cnTensor     *ort.Tensor[float32]

	mu sync.Mutex
}

// NewSileroVAD loads the Silero VAD ONNX model and prepares inference tensors.
// The onnxruntime shared library must already be in PATH or beside the executable.
func NewSileroVAD(modelPath string) (*SileroVAD, error) {
	ort.SetSharedLibraryPath("onnxruntime.dll")
	if err := ort.InitializeEnvironment(); err != nil {
		return nil, fmt.Errorf("onnx env init: %w", err)
	}

	v := &SileroVAD{}
	if err := v.initTensors(); err != nil {
		ort.DestroyEnvironment()
		return nil, err
	}

	if err := v.initSession(modelPath); err != nil {
		v.destroyTensors()
		ort.DestroyEnvironment()
		return nil, err
	}

	return v, nil
}

func (v *SileroVAD) initTensors() error {
	var err error

	// Input: float32[1, FrameSize] -- audio frame normalized [-1, 1]
	inputData := make([]float32, FrameSize)
	v.inputTensor, err = ort.NewTensor(ort.NewShape(1, int64(FrameSize)), inputData)
	if err != nil {
		return fmt.Errorf("input tensor: %w", err)
	}

	// Sample rate: int64[1]
	v.srTensor, err = ort.NewTensor(ort.NewShape(1), []int64{SampleRate})
	if err != nil {
		return fmt.Errorf("sr tensor: %w", err)
	}

	// Hidden state: float32[2, 1, 128]
	v.hTensor, err = ort.NewEmptyTensor[float32](ort.NewShape(2, 1, 128))
	if err != nil {
		return fmt.Errorf("h tensor: %w", err)
	}

	// Cell state: float32[2, 1, 128]
	v.cTensor, err = ort.NewEmptyTensor[float32](ort.NewShape(2, 1, 128))
	if err != nil {
		return fmt.Errorf("c tensor: %w", err)
	}

	// Output: float32[1, 1] -- speech probability
	v.outputTensor, err = ort.NewEmptyTensor[float32](ort.NewShape(1, 1))
	if err != nil {
		return fmt.Errorf("output tensor: %w", err)
	}

	// Hidden state output: float32[2, 1, 128]
	v.hnTensor, err = ort.NewEmptyTensor[float32](ort.NewShape(2, 1, 128))
	if err != nil {
		return fmt.Errorf("hn tensor: %w", err)
	}

	// Cell state output: float32[2, 1, 128]
	v.cnTensor, err = ort.NewEmptyTensor[float32](ort.NewShape(2, 1, 128))
	if err != nil {
		return fmt.Errorf("cn tensor: %w", err)
	}

	return nil
}

func (v *SileroVAD) initSession(modelPath string) error {
	var err error
	v.session, err = ort.NewAdvancedSession(
		modelPath,
		[]string{"input", "sr", "h", "c"},
		[]string{"output", "hn", "cn"},
		[]ort.Value{v.inputTensor, v.srTensor, v.hTensor, v.cTensor},
		[]ort.Value{v.outputTensor, v.hnTensor, v.cnTensor},
		nil,
	)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// ProcessFrame returns speech probability (0.0-1.0) for a single audio frame.
// pcm must contain exactly FrameSize samples of S16 PCM.
func (v *SileroVAD) ProcessFrame(pcm []int16) (float32, error) {
	if len(pcm) != FrameSize {
		return 0, fmt.Errorf("expected %d samples, got %d", FrameSize, len(pcm))
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	// Convert S16 to float32 normalized [-1, 1] directly into tensor data
	inputData := v.inputTensor.GetData()
	for i, s := range pcm {
		inputData[i] = float32(s) / 32768.0
	}

	if err := v.session.Run(); err != nil {
		return 0, fmt.Errorf("inference: %w", err)
	}

	prob := v.outputTensor.GetData()[0]

	// Copy output hidden/cell state back to input state for next frame
	copy(v.hTensor.GetData(), v.hnTensor.GetData())
	copy(v.cTensor.GetData(), v.cnTensor.GetData())

	return prob, nil
}

// Reset clears the hidden state for a new recording session.
func (v *SileroVAD) Reset() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.hTensor.ZeroContents()
	v.cTensor.ZeroContents()
}

func (v *SileroVAD) Close() {
	if v.session != nil {
		v.session.Destroy()
	}
	v.destroyTensors()
	ort.DestroyEnvironment()
}

func (v *SileroVAD) destroyTensors() {
	for _, t := range []interface{ Destroy() error }{
		v.inputTensor, v.srTensor, v.hTensor, v.cTensor,
		v.outputTensor, v.hnTensor, v.cnTensor,
	} {
		if t != nil {
			t.Destroy()
		}
	}
}
