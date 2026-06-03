package main

// objectRunnerEnabled reports whether the native object-model runner is enabled
// (default on; ORUN_OBJECT_RUNNER=0 is the escape hatch).
func objectRunnerEnabled() bool { return flagDefaultOn("ORUN_OBJECT_RUNNER") }
