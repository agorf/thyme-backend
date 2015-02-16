package main

import (
	"crypto/md5"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sync"

	"github.com/cheggaaa/pb"
	_ "github.com/mattn/go-sqlite3"
)

const (
	thumbsDir      = "public/thumbs"
	bigThumbSize   = "1000"
	smallThumbSize = "200"
	workers        = 4 // min: 1
)

func generateSmallThumb(photoPath, identifier string) (thumbPath string, err error) {
	thumbPath = path.Join(thumbsDir, fmt.Sprintf("%s_small.jpg", identifier))

	absThumbPath, err := filepath.Abs(thumbPath)
	if err != nil {
		return
	}

	if _, err = os.Stat(thumbPath); os.IsNotExist(err) { // file does not exist
		err = exec.Command(
			"vipsthumbnail", photoPath,
			"--rotate",
			"--size", smallThumbSize,
			"--crop",
			"--interpolator", "bicubic",
			"--output", absThumbPath+"[Q=97,no_subsample,strip]").Run()
	}

	return
}

func generateBigThumb(photoPath, identifier string) (thumbPath string, err error) {
	thumbPath = path.Join(thumbsDir, fmt.Sprintf("%s_big.jpg", identifier))

	absThumbPath, err := filepath.Abs(thumbPath)
	if err != nil {
		return
	}

	if _, err = os.Stat(thumbPath); os.IsNotExist(err) { // file does not exist
		err = exec.Command(
			"vipsthumbnail", photoPath,
			"--rotate",
			"--size", bigThumbSize,
			"--interpolator", "bicubic",
			"--output", absThumbPath+"[Q=97,no_subsample,strip]").Run()
	}

	return
}

func generateThumbsImpl(photoPath string) (err error) {
	smallThumbPhotoPath := photoPath
	identifier := fmt.Sprintf("%x", md5.Sum([]byte(photoPath)))

	bigThumbPath, err := generateBigThumb(photoPath, identifier)
	if err == nil {
		smallThumbPhotoPath = bigThumbPath // create small thumb from big for speed
	} else {
		log.Println("Failed to create", bigThumbPath, "for", photoPath, "with error:", err)
	}

	smallThumbPath, err := generateSmallThumb(smallThumbPhotoPath, identifier)
	if err != nil {
		log.Println("Failed to create", smallThumbPath, "for", photoPath, "with error:", err)
	}

	return
}

func generateThumbs(ch chan string, wg *sync.WaitGroup, bar *pb.ProgressBar) {
	defer wg.Done()

	for photoPath := range ch {
		generateThumbsImpl(photoPath)
		bar.Increment()
	}
}

func main() {
	var photosCount int

	logFile, err := os.Create("thymian-generate-thumbs.log")
	if err == nil {
		log.SetOutput(logFile)
	}
	defer logFile.Close()

	db, err := sql.Open("sqlite3", "thyme.db")
	if err != nil {
		log.Fatalln("Failed to open database:", err)
	}
	defer db.Close()

	rows, err := db.Query(`
	SELECT path FROM photos
	JOIN sets ON photos.set_id = sets.id
	ORDER BY sets.taken_at DESC, photos.taken_at ASC
	`)
	if err != nil {
		log.Fatalln("Failed to query photos table:", err)
	}
	defer rows.Close()

	err = db.QueryRow("SELECT COUNT(*) FROM photos").Scan(&photosCount)
	if err != nil {
		log.Fatalln("Failed to get photos count:", err)
	}

	err = os.MkdirAll(thumbsDir, os.ModeDir|0755)
	if err != nil {
		log.Fatalln("Failed to create thumbs directory:", err)
	}

	ch := make(chan string)
	wg := sync.WaitGroup{}
	bar := pb.StartNew(photosCount)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go generateThumbs(ch, &wg, bar)
	}

	for rows.Next() {
		var photoPath string
		if err := rows.Scan(&photoPath); err != nil {
			log.Fatalln("Failed to get photo path:", err)
		}
		ch <- photoPath
	}

	close(ch)
	wg.Wait()

	if err := rows.Err(); err != nil {
		log.Fatalln(err)
	}

	// Remove empty log file
	logFileInfo, err := logFile.Stat()
	if err == nil && logFileInfo.Size() == 0 {
		os.Remove(logFileInfo.Name())
	}
}
