package scripts

import (
	"debug/elf"
	"encoding/binary"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBuildScripts_ExecutablePermissions(t *testing.T) {
	for _, script := range []string{"build_termux.sh", "install.sh"} {
		info, err := os.Stat(script)
		if err != nil {
			t.Fatalf("script %s not found: %v", script, err)
		}
		mode := info.Mode()
		if mode&0111 == 0 {
			t.Errorf("script %s is not marked executable (mode: %v)", script, mode)
		}
	}
}

func TestBuildArtifact_ValidARM64ELF(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}

	binPath := filepath.Join(root, "bin", "nexusgate")
	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		t.Skip("bin/nexusgate binary not found, skipping ELF inspection")
		return
	}

	// 1. Inspect ELF binary structure
	f, err := elf.Open(binPath)
	if err != nil {
		t.Fatalf("failed to open ELF binary %s: %v", binPath, err)
	}
	defer f.Close()

	if f.Class != elf.ELFCLASS64 {
		t.Errorf("expected 64-bit ELF class (ELFCLASS64), got %v", f.Class)
	}

	if f.Machine != elf.EM_AARCH64 {
		t.Errorf("expected ARM64 machine target (EM_AARCH64), got %v", f.Machine)
	}

	if f.ByteOrder != binary.LittleEndian {
		t.Errorf("expected LittleEndian byte order, got %v", f.ByteOrder)
	}

	t.Logf("Verified binary %s: ELF64 ARM64 Little-Endian", binPath)

	// 2. If running on arm64 host, execute version --json assertion
	if runtime.GOARCH == "arm64" {
		cmd := exec.Command(binPath, "version", "--json")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("failed to execute binary: %v", err)
		}

		var verInfo map[string]string
		if err := json.Unmarshal(out, &verInfo); err != nil {
			t.Fatalf("failed to parse version json: %v, raw: %s", err, string(out))
		}

		if verInfo["version"] != "1.0.0" {
			t.Errorf("expected version 1.0.0, got %s", verInfo["version"])
		}

		if !strings.Contains(verInfo["platform"], "arm64") {
			t.Errorf("expected platform to contain arm64, got %s", verInfo["platform"])
		}

		if verInfo["target"] != "termux/arm64" {
			t.Errorf("expected target termux/arm64, got %s", verInfo["target"])
		}
	}
}
