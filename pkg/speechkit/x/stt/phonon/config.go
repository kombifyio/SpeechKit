package phonon

// Config configures engine.NewRecognizer.
type Config struct {
	// ModelDir is the absolute path of a Phonon model directory: Fermion's
	// unpacked archive with model.fermion, config.json and
	// packed_manifest.json (see [ResolveModelDir]).
	ModelDir string
	// Threads is the number of CPU threads the model runs on; values below 1
	// use one thread.
	Threads int
}
