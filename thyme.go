package main

import (
	"os"

	"github.com/agorf/thyme-backend/photos"
	"github.com/agorf/thyme-backend/server"
	"github.com/agorf/thyme-backend/thumbs"
)

func main() {
	if len(os.Args) < 2 {
		return
	}

	thymePath := "."
	if len(os.Args) > 2 {
		thymePath = os.Args[2]
	}

	switch os.Args[1] {
	case "scan-photos":
		photos.ScanPhotos(os.Args[2:]...)
	case "generate-thumbs":
		thumbs.GenerateThumbs(thymePath)
	case "serve":
		server.Serve(thymePath)
	}
}
