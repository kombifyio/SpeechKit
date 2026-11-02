package procguard

import (
	"errors"
	"io"
	"os/exec"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
)

// ErrNetworkConfinementRequired means the deployment must supply inherited
// child-process confinement. The Go dial guard, a loopback listen address,
// scrubbed environment and kill-on-exit custody cannot constrain native code.
var ErrNetworkConfinementRequired = errors.New("data_profile_full_local: native runtime requires kernel-enforced child network confinement")

// ForwardOutput makes exec create a pipe and forward through the parent under
// Full Local. A journal's socket descriptor must never be inherited by native
// code. Standard retains the original stream and lifecycle behavior.
func ForwardOutput(writer io.Writer) io.Writer {
	if writer == nil || !netsec.FullLocal() {
		return writer
	}
	return struct{ io.Writer }{writer}
}

// RequireSpeechConfinement checks inexpensive host prerequisites. It never
// attests a deployment: Start must arm and verify the kernel for every launch.
func RequireSpeechConfinement() error {
	if netsec.FullLocal() {
		return confinementAvailable()
	}
	return nil
}

// Start is the native launch boundary. Full Local never delegates a prepared
// command back to a caller for an unguarded Start. Standard retains Prepare's
// existing platform lifecycle behavior.
func Start(cmd *exec.Cmd) error {
	if netsec.FullLocal() {
		return startConfined(cmd)
	}
	Prepare(cmd)
	return cmd.Start()
}

// Run is Start followed by Wait, including short-lived health probes.
func Run(cmd *exec.Cmd) error {
	if err := Start(cmd); err != nil {
		return err
	}
	return cmd.Wait()
}

// PrepareChildRead hands a newly-created private runtime identity file or
// directory to the confined child identity. Callers retain removal custody.
// Standard permissions and shared model assets are unchanged.
func PrepareChildRead(path string) error {
	if !netsec.FullLocal() {
		return nil
	}
	return prepareChildRead(path)
}

// Prepare retains the lifecycle API for existing consumers. Under Full Local a
// legacy Prepare + cmd.Start sequence is deliberately poisoned: only Start can
// atomically arm and launch. Ignoring a return value cannot bypass confinement.
func Prepare(cmd *exec.Cmd) {
	if netsec.FullLocal() {
		if cmd != nil {
			cmd.Err = ErrNetworkConfinementRequired
		}
		return
	}
	preparePlatform(cmd)
}
