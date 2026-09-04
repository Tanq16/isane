package daemon

import (
	"fmt"
	"path/filepath"
)

const answerInstruction = "Write your final answer to %s. That file is the only thing sent back, " +
	"so it has to carry the complete answer on its own. Overwrite it whole and ignore whatever it already holds."

func composePrompt(dir, body string) string {
	return body + "\n\n" + fmt.Sprintf(answerInstruction, filepath.Join(dir, resultFile))
}
