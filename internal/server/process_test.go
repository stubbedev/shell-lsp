package server

import (
	"testing"

	"github.com/stubbedev/shell-lsp/internal/gitrepo"
)

func TestTheServerDiscoversRepositoriesWhateverTheGitDirectoryVariablesSay(t *testing.T) {
	t.Parallel()

	p := Process{WorkDir: t.TempDir(), Git: gitrepo.Process{Variables: map[string]string{"GIT_DIR": "elsewhere", "GIT_WORK_TREE": "", "GIT_COMMON_DIR": "x"}}}
	if got := p.git().Repositories; got != gitrepo.Discovery() {
		t.Errorf("the server's repository lookup = %+v, want discovery", got)
	}
}
