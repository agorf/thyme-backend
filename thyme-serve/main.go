package main

import (
	"crypto/md5"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"

	_ "github.com/mattn/go-sqlite3"
)

const listenAddr = "127.0.0.1:9292"

var (
	db         *sql.DB
	getSetStmt *sql.Stmt
)

type Set struct {
	Id             int
	ThumbPhotoId   int
	Name           string
	PhotosCount    int
	TakenAt        sql.NullString
	ThumbPhotoPath string
}

func (s *Set) ThumbURL() string {
	identifier := fmt.Sprintf("%x", md5.Sum([]byte(s.ThumbPhotoPath)))
	return fmt.Sprintf("/thumbs/%s_small.jpg", identifier)
}

func (s *Set) MarshalJSON() ([]byte, error) { // implements Marshaler
	setMap := map[string]interface{}{
		"id":             s.Id,
		"thumb_photo_id": s.ThumbPhotoId,
		"name":           s.Name,
		"photos_count":   s.PhotosCount,
		"thumb_url":      s.ThumbURL(),
	}
	setMap["taken_at"], _ = s.TakenAt.Value()
	return json.Marshal(setMap)
}

func badRequest(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusBadRequest)
	fmt.Fprintln(w, http.StatusBadRequest, "Bad Request")
}

func requireParam(param string, w http.ResponseWriter, r *http.Request) error {
	if len(r.URL.Query()[param]) == 0 {
		badRequest(w, r)
		return errors.New(fmt.Sprintf("missing %s parameter", param))
	}
	return nil
}

func getSetById(setId int) (*Set, error) {
	set := Set{}
	row := getSetStmt.QueryRow(setId)
	err := row.Scan(
		&set.Id,
		&set.ThumbPhotoId,
		&set.Name,
		&set.PhotosCount,
		&set.TakenAt,
		&set.ThumbPhotoPath,
	)
	return &set, err
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(set)
}

func main() {
	db, err := sql.Open("sqlite3", "thyme.db")
	if err != nil {
		log.Fatalln("Failed to open database:", err)
	}
	defer db.Close()

	getSetStmt, err = db.Prepare(`
	SELECT sets.id, thumb_photo_id, name, photos_count, sets.taken_at, photos.path
	FROM sets JOIN photos ON sets.thumb_photo_id = photos.id
	WHERE sets.id = ?
	`)
	if err != nil {
		log.Fatalln("Failed to access table:", err)
	}
	defer getSetStmt.Close()

	http.Handle("/", http.FileServer(http.Dir("public"))) // static
	http.HandleFunc("/set", getSetHandler)

	fmt.Printf("Listening on http://%s\n", listenAddr)
	fmt.Println("Press Ctrl-C to exit")

	log.Fatal(http.ListenAndServe(listenAddr, nil))
}
