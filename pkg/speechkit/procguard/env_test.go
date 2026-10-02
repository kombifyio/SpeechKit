package procguard_test

import (
	"slices"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/procguard"
)

// A sidecar must start (OS basics, GPU runtime knobs) without seeing the
// host's credentials or llama-server flag overrides.
func TestSidecarEnvKeepsRuntimeAndDropsCredentials(t *testing.T) {
	t.Parallel()
	environ := []string{
		"PATH=/usr/bin", "SystemRoot=C:\\Windows", "TEMP=C:\\t", "TMP=C:\\t", "HOME=/home/u",
		"USERPROFILE=C:\\Users\\u", "LANG=de_DE.UTF-8", "LC_ALL=C", "CUDA_VISIBLE_DEVICES=0",
		"HIP_VISIBLE_DEVICES=0", "VULKAN_SDK=/vk", "GGML_METAL_PATH_RESOURCES=/r", "=C:=C:\\work",
		"OPENAI_API_KEY=sk", "HF_TOKEN=hf", "CLOUDFLARE_API_TOKEN=cf", "AWS_SECRET_ACCESS_KEY=a",
		"AZURE_CLIENT_SECRET=z", "GOOGLE_APPLICATION_CREDENTIALS=/g.json", "DOPPLER_TOKEN=d",
		"LLAMA_ARG_ENDPOINT_SLOTS=1", "LLAMA_API_KEY=x", "SPEECHKIT_SERVER_TOKEN=s", "MY_PROVIDER=custom",
	}
	got := procguard.SidecarEnv(environ, "my_provider")

	for _, keep := range []string{
		"PATH=/usr/bin", "SystemRoot=C:\\Windows", "TEMP=C:\\t", "TMP=C:\\t", "HOME=/home/u",
		"USERPROFILE=C:\\Users\\u", "LANG=de_DE.UTF-8", "LC_ALL=C", "CUDA_VISIBLE_DEVICES=0",
		"HIP_VISIBLE_DEVICES=0", "VULKAN_SDK=/vk", "GGML_METAL_PATH_RESOURCES=/r", "=C:=C:\\work",
	} {
		if !slices.Contains(got, keep) {
			t.Errorf("SidecarEnv dropped %q", keep)
		}
	}
	for _, drop := range []string{
		"OPENAI_API_KEY=sk", "HF_TOKEN=hf", "CLOUDFLARE_API_TOKEN=cf", "AWS_SECRET_ACCESS_KEY=a",
		"AZURE_CLIENT_SECRET=z", "GOOGLE_APPLICATION_CREDENTIALS=/g.json", "DOPPLER_TOKEN=d",
		"LLAMA_ARG_ENDPOINT_SLOTS=1", "LLAMA_API_KEY=x", "SPEECHKIT_SERVER_TOKEN=s", "MY_PROVIDER=custom",
	} {
		if slices.Contains(got, drop) {
			t.Errorf("SidecarEnv kept %q", drop)
		}
	}
}
