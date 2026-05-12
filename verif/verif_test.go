package verif

import (
	"testing"
)

func Test_GoVet(t *testing.T) {
	out, err := runGo("vet", "./...")
	if err != nil {
		t.Fatalf("go vet failed:\n%s\n%v", out, err)
	}
}

func Test_GoBuild(t *testing.T) {
	out, err := runGo("build", "-race", "-o", "/dev/null", "./cmd/server/")
	if err != nil {
		t.Fatalf("go build failed:\n%s\n%v", out, err)
	}
}

func Test_GoTests(t *testing.T) {
	out, err := runGo("test", "-race", "-count=1", "./internal/...")
	if err != nil {
		t.Fatalf("go test failed:\n%s\n%v", out, err)
	}
}
