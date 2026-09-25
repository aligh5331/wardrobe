package tests

import "runtime"

// exeSuffix lets the process-level harnesses name built binaries so Windows can
// execute them: go build -o writes exactly the given name, and Windows'
// CreateProcess/LookPath require an .exe extension, so an extensionless binary
// fails with "executable file not found". Empty on other platforms.
func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}
