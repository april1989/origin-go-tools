package imports

import (
	"os"
	"testing"

	"github.com/o2lab/go-tools/internal/testenv"
)

func TestMain(m *testing.M) {
	testenv.ExitIfSmallMachine()
	os.Exit(m.Run())
}
