package httpapi

import (
	"io/fs"
	"net/http"
)

func SPAHandler(dist fs.FS, placeholder []byte) http.Handler { return spaHandler(dist, placeholder) }
