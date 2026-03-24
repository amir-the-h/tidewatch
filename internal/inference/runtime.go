package inference

import (
	"fmt"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

var (
	runtimeOnce sync.Once
	runtimeErr  error
)

// InitRuntime initializes the global ONNX Runtime environment. Must be called
// once at app startup before any Pipeline.Load calls. libPath is the path to
// the onnxruntime shared library (e.g. "/usr/local/lib/libonnxruntime.so.1.24.1").
// If libPath is empty the default "onnxruntime.so" is used.
func InitRuntime(libPath string) error {
	runtimeOnce.Do(func() {
		if libPath != "" {
			ort.SetSharedLibraryPath(libPath)
		}
		runtimeErr = ort.InitializeEnvironment()
		if runtimeErr != nil {
			runtimeErr = fmt.Errorf("inference: runtime: init: %w", runtimeErr)
		}
	})
	return runtimeErr
}

// DestroyRuntime releases the ONNX Runtime environment. Call once on shutdown.
func DestroyRuntime() error {
	if err := ort.DestroyEnvironment(); err != nil {
		return fmt.Errorf("inference: runtime: destroy: %w", err)
	}
	return nil
}
