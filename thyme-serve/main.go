package main

import (
	"crypto/md5"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"math/big"
	"net/http"
	"os"
	"path"
	"strconv"

	"github.com/gorilla/handlers"
	_ "github.com/mattn/go-sqlite3"
)

const (
	listenAddr     = "127.0.0.1:9292"
	bigThumbSize   = 1000
	smallThumbSize = 200
)

var (
	db            *sql.DB
	getSetStmt    *sql.Stmt
	getSetsStmt   *sql.Stmt
	getPhotoStmt  *sql.Stmt
	getPhotosStmt *sql.Stmt
)

type Set struct {
	Id             int
	Name           string
	PhotosCount    int
	TakenAt        sql.NullString
	ThumbPhotoId   int
	ThumbPhotoPath string
}

type Photo struct {
	Aperture      sql.NullFloat64
	Camera        sql.NullString
	ExposureComp  sql.NullInt64
	ExposureTime  sql.NullFloat64
	Flash         sql.NullString
	FocalLength   sql.NullFloat64
	FocalLength35 sql.NullInt64
	Height        int64
	ISO           sql.NullInt64
	Id            int
	Lat           sql.NullFloat64
	Lens          sql.NullString
	Lng           sql.NullFloat64
	NextPhotoId   sql.NullInt64
	Path          string
	PrevPhotoId   sql.NullInt64
	SetId         int
	Size          int
	TakenAt       sql.NullString
	Width         int64
}

// used by scanSet and scanPhoto to accept row(s)
type RowScanner interface {
	Scan(dest ...interface{}) error
}

func thumbURL(photoPath, suffix string) string {
	identifier := fmt.Sprintf("%x", md5.Sum([]byte(photoPath)))
	return fmt.Sprintf("/thumbs/%s_%s.jpg", identifier, suffix)
}

func (s *Set) ThumbURL() string {
	return thumbURL(s.ThumbPhotoPath, "small")
}

func (s *Set) MarshalJSON() ([]byte, error) { // implements Marshaler
	setMap := map[string]interface{}{
		"id":             s.Id,
		"name":           s.Name,
		"photos_count":   s.PhotosCount,
		"thumb_photo_id": s.ThumbPhotoId,
		"thumb_url":      s.ThumbURL(),
	}
	setMap["taken_at"], _ = s.TakenAt.Value()
	return json.Marshal(setMap)
}

func (p *Photo) AspectRatio() [2]int64 {
	gcd := new(big.Int).GCD(nil, nil, big.NewInt(p.Width), big.NewInt(p.Height)).Int64()
	return [2]int64{p.Width / gcd, p.Height / gcd}
}

func (p *Photo) BigThumbHeight() int64 {
	if p.Orientation() == "portrait" {
		if p.Height < bigThumbSize {
			return p.Height
		}

		return bigThumbSize
	}

	aspectRatio := p.AspectRatio()
	return int64(math.Floor((float64(aspectRatio[1])/float64(aspectRatio[0]))*float64(p.BigThumbWidth()) + .5))
}

func (p *Photo) BigThumbWidth() int64 {
	if p.Orientation() == "portrait" {
		aspectRatio := p.AspectRatio()
		return int64(math.Floor((float64(aspectRatio[0])/float64(aspectRatio[1]))*float64(p.BigThumbHeight()) + .5))
	}

	if p.Width < bigThumbSize {
		return p.Width
	}

	return bigThumbSize
}

func (p *Photo) Filename() string {
	return path.Base(p.Path)
}

func (p *Photo) Orientation() string {
	if p.Height > p.Width {
		return "portrait"
	}
	return "landscape"
}

func (p *Photo) ThumbURL(suffix string) string {
	return thumbURL(p.Path, suffix)
}

func (p *Photo) MarshalJSON() ([]byte, error) { // implements Marshaler
	photoMap := map[string]interface{}{
		"aspect_ratio":     p.AspectRatio(),
		"big_thumb_height": p.BigThumbHeight(),
		"big_thumb_url":    p.ThumbURL("big"),
		"big_thumb_width":  p.BigThumbWidth(),
		"filename":         p.Filename(),
		"height":           p.Height,
		"id":               p.Id,
		"orientation":      p.Orientation(),
		"path":             p.Path,
		"set_id":           p.SetId,
		"size":             p.Size,
		"small_thumb_url":  p.ThumbURL("small"),
		"width":            p.Width,
	}

	photoMap["aperture"], _ = p.Aperture.Value()
	photoMap["camera"], _ = p.Camera.Value()
	photoMap["exposure_comp"], _ = p.ExposureComp.Value()
	photoMap["exposure_time"], _ = p.ExposureTime.Value()
	photoMap["flash"], _ = p.Flash.Value()
	photoMap["focal_length"], _ = p.FocalLength.Value()
	photoMap["focal_length_35"], _ = p.FocalLength35.Value()
	photoMap["iso"], _ = p.ISO.Value()
	photoMap["lat"], _ = p.Lat.Value()
	photoMap["lens"], _ = p.Lens.Value()
	photoMap["lng"], _ = p.Lng.Value()
	photoMap["next_photo_id"], _ = p.NextPhotoId.Value()
	photoMap["prev_photo_id"], _ = p.PrevPhotoId.Value()
	photoMap["taken_at"], _ = p.TakenAt.Value()

	return json.Marshal(photoMap)
}

func badRequest(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusBadRequest)
	fmt.Fprintln(w, http.StatusBadRequest, "Bad Request")
}

func internalServerError(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusInternalServerError)
	fmt.Fprintln(w, http.StatusInternalServerError, "Internal Server Error")
}

func requireParam(param string, w http.ResponseWriter, r *http.Request) error {
	if len(r.URL.Query()[param]) == 0 {
		badRequest(w, r)
		return errors.New(fmt.Sprintf("missing %s parameter", param))
	}
	return nil
}

func scanSet(row RowScanner, set *Set) error {
	return row.Scan(
		&set.Id,
		&set.Name,
		&set.PhotosCount,
		&set.TakenAt,
		&set.ThumbPhotoId,
		&set.ThumbPhotoPath,
	)
}

func getSetById(setId int) (set *Set, err error) {
	set = &Set{}
	row := getSetStmt.QueryRow(setId)
	err = scanSet(row, set)
	return
}

func getSets() (sets []*Set, err error) {
	rows, err := getSetsStmt.Query()
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		set := Set{}
		if err = scanSet(rows, &set); err != nil {
			return
		}
		sets = append(sets, &set)
	}

	err = rows.Err()

	return
}

func scanPhoto(row RowScanner, photo *Photo) error {
	return row.Scan(
		&photo.Aperture,
		&photo.Camera,
		&photo.ExposureComp,
		&photo.ExposureTime,
		&photo.Flash,
		&photo.FocalLength,
		&photo.FocalLength35,
		&photo.Height,
		&photo.Id,
		&photo.ISO,
		&photo.Lat,
		&photo.Lens,
		&photo.Lng,
		&photo.NextPhotoId,
		&photo.Path,
		&photo.PrevPhotoId,
		&photo.SetId,
		&photo.Size,
		&photo.TakenAt,
		&photo.Width,
	)
}

func getPhotoById(photoId int) (photo *Photo, err error) {
	photo = &Photo{}
	row := getPhotoStmt.QueryRow(photoId)
	err = scanPhoto(row, photo)
	return
}

func getPhotosBySetId(setId int) (photos []*Photo, err error) {
	rows, err := getPhotosStmt.Query(setId)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		photo := Photo{}
		if err = scanPhoto(rows, &photo); err != nil {
			return
		}
		photos = append(photos, &photo)
	}

	err = rows.Err()

	return
}

func getSetHandler(w http.ResponseWriter, r *http.Request) {
	if requireParam("id", w, r) != nil {
		return
	}

	setId, err := strconv.Atoi(r.URL.Query()["id"][0])
	if err != nil {
		log.Fatalln("Failed to convert id to integer:", err)
	}

	set, err := getSetById(setId)
	if err == sql.ErrNoRows { // set does not exist
		http.NotFound(w, r)
		return
	}
	if err != nil {
		internalServerError(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(set)
}

func getSetsHandler(w http.ResponseWriter, r *http.Request) {
	sets, err := getSets()
	if err != nil {
		internalServerError(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sets)
}

func getPhotoHandler(w http.ResponseWriter, r *http.Request) {
	if requireParam("id", w, r) != nil {
		return
	}

	photoId, err := strconv.Atoi(r.URL.Query()["id"][0])
	if err != nil {
		log.Fatalln("Failed to convert id to integer:", err)
	}

	photo, err := getPhotoById(photoId)
	if err == sql.ErrNoRows { // photo does not exist
		http.NotFound(w, r)
		return
	}
	if err != nil {
		internalServerError(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(photo)
}

func getPhotosHandler(w http.ResponseWriter, r *http.Request) {
	if requireParam("set_id", w, r) != nil {
		return
	}

	setId, err := strconv.Atoi(r.URL.Query()["set_id"][0])
	if err != nil {
		log.Fatalln("Failed to convert id to integer:", err)
	}

	photos, err := getPhotosBySetId(setId)
	if err != nil {
		internalServerError(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(photos)
}

func setupDatabase() {
	db, err := sql.Open("sqlite3", "thyme.db")
	if err != nil {
		log.Fatalln("Failed to open database:", err)
	}

	setAttrs := `sets.id, name, photos_count, sets.taken_at, thumb_photo_id,
	photos.path`

	getSetStmt, err = db.Prepare(fmt.Sprintf(`
	SELECT %s FROM sets
	JOIN photos ON sets.thumb_photo_id = photos.id
	WHERE sets.id = ?
	`, setAttrs))
	if err != nil {
		log.Fatalln("Failed to access table:", err)
	}

	getSetsStmt, err = db.Prepare(fmt.Sprintf(`
	SELECT %s FROM sets
	JOIN photos ON sets.thumb_photo_id = photos.id
	ORDER BY sets.taken_at DESC
	`, setAttrs))
	if err != nil {
		log.Fatalln("Failed to access table:", err)
	}

	photoAttrs := `aperture, camera, exposure_comp, exposure_time, flash,
	focal_length, focal_length_35, height, id, iso, lat, lens, lng,
	next_photo_id, path, prev_photo_id, set_id, size, taken_at, width`

	getPhotoStmt, err = db.Prepare(fmt.Sprintf(`
	SELECT %s FROM photos WHERE id = ?
	`, photoAttrs))
	if err != nil {
		log.Fatalln("Failed to access table:", err)
	}

	getPhotosStmt, err = db.Prepare(fmt.Sprintf(`
	SELECT %s FROM photos WHERE set_id = ?
	`, photoAttrs))
	if err != nil {
		log.Fatalln("Failed to access table:", err)
	}
}

func main() {
	setupDatabase()
	defer db.Close()
	defer getSetStmt.Close()
	defer getSetsStmt.Close()
	defer getPhotoStmt.Close()
	defer getPhotosStmt.Close()

	http.Handle("/", http.FileServer(http.Dir("public"))) // static
	http.HandleFunc("/set", getSetHandler)
	http.HandleFunc("/sets", getSetsHandler)
	http.HandleFunc("/photo", getPhotoHandler)
	http.HandleFunc("/photos", getPhotosHandler)

	fmt.Printf("Listening on http://%s\n", listenAddr)
	fmt.Println("Press Ctrl-C to exit")

	log.Fatal(http.ListenAndServe(listenAddr, handlers.LoggingHandler(os.Stdout, http.DefaultServeMux)))
}
