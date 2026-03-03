package detector

// ConfigureOrtRuntime should be called once at app startup.
func ConfigureOrtRuntime(cpuLibPath, gpuLibPath, gpuLibDir string) {
	rt.Configure(cpuLibPath, gpuLibPath, gpuLibDir)
}
