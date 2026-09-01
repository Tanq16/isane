package isane

import (
	"embed"
	"fmt"
	"io/fs"
)

//go:embed all:web
var webFS embed.FS

func WebFS() fs.FS {
	root, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(fmt.Sprintf("embedded web root: %v", err))
	}
	return root
}
