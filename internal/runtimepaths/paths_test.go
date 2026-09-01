package runtimepaths

import (
	"path/filepath"
	"testing"
)

func TestFromLocalAppData(t *testing.T) {
	base := filepath.Join("C:", "Users", "alice", "AppData", "Local")
	got, err := FromLocalAppData(base)
	if err != nil {
		t.Fatal(err)
	}
	wantDir := filepath.Join(base, "Discord Unlocker")
	if got.DataDir != wantDir {
		t.Fatalf("DataDir = %q, want %q", got.DataDir, wantDir)
	}
	if filepath.Dir(got.CacheFile) != wantDir || filepath.Dir(got.LogFile) != wantDir {
		t.Fatalf("arquivos escaparam da pasta de dados: %#v", got)
	}
}

func TestFromLocalAppDataRejectsEmptyPath(t *testing.T) {
	if _, err := FromLocalAppData(""); err == nil {
		t.Fatal("esperava erro para caminho vazio")
	}
}
