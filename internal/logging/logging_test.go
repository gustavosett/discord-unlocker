package logging

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenRotatesOversizedLogAndMirrorsManualOutput(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, "discord-unlocker.log")
	oldContents := bytes.Repeat([]byte{'x'}, maxLogBytes+1)
	if err := os.WriteFile(path, oldContents, 0o600); err != nil {
		t.Fatal(err)
	}

	var console bytes.Buffer
	logger, err := Open(path, &console)
	if err != nil {
		t.Fatal(err)
	}
	logger.Printf("mensagem nova")
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}

	rotated, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatalf("log rotacionado não existe: %v", err)
	}
	if !bytes.Equal(rotated, oldContents) {
		t.Fatal("conteúdo antigo não foi preservado no primeiro backup")
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(current), "mensagem nova") || !strings.Contains(console.String(), "mensagem nova") {
		t.Fatalf("saídas não receberam a mensagem: arquivo=%q console=%q", current, console.String())
	}
}
