package photos

import (
	"database/sql"
	"fmt"
	"image"
	_ "image/jpeg"
	"log"
	"mime"
	"os"
	"path"
	"path/filepath"

	"github.com/agorf/goexif/exif"
	_ "github.com/mattn/go-sqlite3"
)

const createSchemaSQL = `
CREATE TABLE IF NOT EXISTS sets (
	id integer NOT NULL PRIMARY KEY,
	thumb_photo_id integer UNIQUE REFERENCES photos,
	name varchar(4096) NOT NULL UNIQUE,
	photos_count integer,
	taken_at char(19)
);

CREATE UNIQUE INDEX IF NOT EXISTS sets_thumb_photo_id_index ON sets (thumb_photo_id);

CREATE TABLE IF NOT EXISTS photos (
	id integer NOT NULL PRIMARY KEY,
	set_id integer NOT NULL REFERENCES sets,
	prev_photo_id integer UNIQUE REFERENCES photos,
	next_photo_id integer UNIQUE REFERENCES photos,
	path varchar(4096) NOT NULL UNIQUE,
	size integer NOT NULL,
	width integer NOT NULL,
	height integer NOT NULL,
	aperture decimal(2, 1),
	camera varchar(1000),
	exposure_comp integer,
	exposure_time decimal(9, 5),
	flash varchar(51),
	focal_length decimal(3, 1),
	focal_length_35 integer,
	iso integer,
	lat decimal(9, 6),
	lens varchar(1000),
	lng decimal(9, 6),
	taken_at char(19)
);

CREATE INDEX IF NOT EXISTS photos_set_id_index ON photos (set_id);

CREATE UNIQUE INDEX IF NOT EXISTS photos_prev_photo_id_index ON photos (prev_photo_id);

CREATE UNIQUE INDEX IF NOT EXISTS photos_next_photo_id_index ON photos (next_photo_id);

CREATE UNIQUE INDEX IF NOT EXISTS photos_path_index ON photos (path);
`

var (
	db              *sql.DB
	selectSetStmt   *sql.Stmt
	selectPhotoStmt *sql.Stmt
	insertSetStmt   *sql.Stmt
	insertPhotoStmt *sql.Stmt
)

type Photo struct {
	Aperture      sql.NullFloat64
	Camera        sql.NullString
	ExposureComp  sql.NullInt64
	ExposureTime  sql.NullFloat64
	Flash         sql.NullString
	FocalLength   sql.NullFloat64
	FocalLength35 sql.NullInt64
	Height        int
	ISO           sql.NullInt64
	Lat           sql.NullFloat64
	Lens          sql.NullString
	Lng           sql.NullFloat64
	Path          string
	Size          int64
	TakenAt       sql.NullString
	Width         int
}

func (p *Photo) decodeExif(x *exif.Exif) {
	takenAt, err := x.DateTime()
	if err == nil {
		p.TakenAt.String = takenAt.UTC().Format("2006-01-02 15:04:05")
		p.TakenAt.Valid = true
	}

	lat, lng, err := x.LatLong()
	if err == nil {
		p.Lat.Float64 = lat
		p.Lat.Valid = true
		p.Lng.Float64 = lng
		p.Lng.Valid = true
	}

	orientTag, err := x.Get(exif.Orientation)
	if err == nil {
		switch orient, _ := orientTag.Int(0); orient {
		case 5, 6, 7, 8: // rotated
			p.Width, p.Height = p.Height, p.Width // swap
		}
	}

	camMakeTag, err := x.Get(exif.Make)
	if err == nil {
		p.Camera.String, _ = camMakeTag.StringVal()
		p.Camera.Valid = true
	}

	camModelTag, err := x.Get(exif.Model)
	if err == nil {
		cameraModel, _ := camModelTag.StringVal()

		if p.Camera.Valid {
			p.Camera.String = fmt.Sprint(p.Camera.String, " ", cameraModel)
		} else {
			p.Camera.String = cameraModel
			p.Camera.Valid = true
		}
	}

	lensMakeTag, err := x.Get(exif.LensMake)
	if err == nil {
		p.Lens.String, _ = lensMakeTag.StringVal()
		p.Lens.Valid = true
	}

	lensModelTag, err := x.Get(exif.LensModel)
	if err == nil {
		lensModel, _ := lensModelTag.StringVal()

		if p.Lens.Valid {
			p.Lens.String = fmt.Sprint(p.Lens.String, " ", lensModel)
		} else {
			p.Lens.String = lensModel
			p.Lens.Valid = true
		}
	}

	focalLenTag, err := x.Get(exif.FocalLength)
	if err == nil {
		focalLen, _ := focalLenTag.Rat(0)
		p.FocalLength.Float64, _ = focalLen.Float64()
		p.FocalLength.Valid = true
	}

	focalLen35Tag, err := x.Get(exif.FocalLengthIn35mmFilm)
	if err == nil {
		p.FocalLength35.Int64, _ = focalLen35Tag.Int64(0)
		p.FocalLength35.Valid = true
	}

	apertureTag, err := x.Get(exif.FNumber)
	if err == nil {
		aperture, _ := apertureTag.Rat(0)
		p.Aperture.Float64, _ = aperture.Float64()
		p.Aperture.Valid = true
	}

	expTimeTag, err := x.Get(exif.ExposureTime)
	if err == nil {
		expTime, _ := expTimeTag.Rat(0)
		p.ExposureTime.Float64, _ = expTime.Float64()
		p.ExposureTime.Valid = true
	}

	isoTag, err := x.Get(exif.ISOSpeedRatings)
	if err == nil {
		p.ISO.Int64, _ = isoTag.Int64(0)
		p.ISO.Valid = true
	}

	expBiasTag, err := x.Get(exif.ExposureBiasValue)
	if err == nil {
		p.ExposureComp.Int64, _ = expBiasTag.Int64(0)
		p.ExposureComp.Valid = true
	}

	flash, err := x.Flash()
	if err == nil {
		p.Flash.String = flash
		p.Flash.Valid = true
	}
}

func (p *Photo) decode(path string) error {
	p.Path = path

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return err
	}
	p.Size = fi.Size()

	img, _, err := image.DecodeConfig(f)
	if err != nil {
		return err
	}
	p.Width, p.Height = img.Width, img.Height

	f.Seek(0, 0) // rewind

	x, err := exif.Decode(f)
	if err == nil { // EXIF data exists
		p.decodeExif(x)
	}

	return nil
}

func (p *Photo) store() error {
	var setId, photoId int64

	setName := filepath.Base(filepath.Dir(p.Path))
	row := selectSetStmt.QueryRow(setName)
	if err := row.Scan(&setId); err == sql.ErrNoRows { // set does not exist
		result, err := insertSetStmt.Exec(setName) // create it
		if err != nil {
			return err
		}

		setId, err = result.LastInsertId()
		if err != nil {
			return err
		}
	}

	row = selectPhotoStmt.QueryRow(p.Path)
	if err := row.Scan(&photoId); err == sql.ErrNoRows { // photo does not exist
		result, err := insertPhotoStmt.Exec(p.Aperture, p.Camera,
			p.ExposureComp, p.ExposureTime, p.Flash, p.FocalLength,
			p.FocalLength35, p.Height, p.ISO, p.Lat, p.Lens,
			p.Lng, p.Path, setId, p.Size, p.TakenAt, p.Width) // create it
		if err != nil {
			return err
		}

		photoId, err = result.LastInsertId()
		if err != nil {
			return err
		}

		fmt.Printf("photos id=%d path=%s\n", photoId, p.Path)
	}

	return nil
}

func isPhoto(path string, info os.FileInfo) bool {
	if info.IsDir() {
		return false
	}

	if mime.TypeByExtension(filepath.Ext(path)) != "image/jpeg" { // not JPEG
		return false
	}

	return true
}

func walkPath(path string, info os.FileInfo, err error) error {
	if err != nil { // error walking "path"
		return nil // skip
	}

	if !isPhoto(path, info) {
		return nil // skip
	}

	photo := &Photo{}
	if err := photo.decode(path); err == nil {
		photo.store()
	}

	return nil // next
}

func updatePhotoSiblings() error {
	var prevId, prevSetId int

	tx, err := db.Begin()
	if err != nil {
		return err
	}

	updatePrevPhotoStmt, err := tx.Prepare(`
	UPDATE photos SET prev_photo_id = ? WHERE id = ?
	`)
	if err != nil {
		return err
	}
	defer updatePrevPhotoStmt.Close()

	updateNextPhotoStmt, err := tx.Prepare(`
	UPDATE photos SET next_photo_id = ? WHERE id = ?
	`)
	if err != nil {
		return err
	}
	defer updateNextPhotoStmt.Close()

	rows, err := tx.Query(`
	SELECT id, set_id FROM photos ORDER BY set_id, taken_at
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var id, setId int
		rows.Scan(&id, &setId)

		if setId == prevSetId && prevId > 0 {
			updatePrevPhotoStmt.Exec(prevId, id)
			fmt.Printf("photos id=%d prev_photo_id=%d\n", id, prevId)
			updateNextPhotoStmt.Exec(id, prevId)
			fmt.Printf("photos id=%d next_photo_id=%d\n", prevId, id)
		}

		prevId = id
		prevSetId = setId
	}

	if err := rows.Err(); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func updateSets() error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}

	photosCountStmt, err := tx.Prepare(`
	SELECT COUNT(*) FROM photos WHERE set_id = ?
	`)
	if err != nil {
		return err
	}
	defer photosCountStmt.Close()

	updateSetStmt, err := tx.Prepare(`
	UPDATE sets
	SET photos_count = ?, taken_at = ?, thumb_photo_id = ?
	WHERE id = ?
	`)
	if err != nil {
		return err
	}
	defer updateSetStmt.Close()

	rows, err := tx.Query(`
	SELECT id, set_id, MIN(taken_at) FROM photos GROUP BY set_id
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var id, setId, photosCount int
		var takenAt sql.NullString

		rows.Scan(&id, &setId, &takenAt)

		row := photosCountStmt.QueryRow(setId)
		row.Scan(&photosCount)

		updateSetStmt.Exec(photosCount, takenAt, id, setId)
		fmt.Printf("sets id=%d photos_count=%d taken_at=%q thumb_photo_id=%d\n", setId, photosCount, takenAt.String, id)
	}

	if err := rows.Err(); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func setupDatabase() {
	var err error

	dbPath := path.Join(os.Getenv("HOME"), ".thyme.db")
	db, err = sql.Open("sqlite3", dbPath) // := here shadows global db var
	if err != nil {
		log.Fatal(err)
	}

	_, err = db.Exec(createSchemaSQL)
	if err != nil {
		log.Fatal(err)
	}

	selectSetStmt, err = db.Prepare("SELECT id FROM sets WHERE name = ?")
	if err != nil {
		log.Fatal(err)
	}

	selectPhotoStmt, err = db.Prepare("SELECT id FROM photos WHERE path = ?")
	if err != nil {
		log.Fatal(err)
	}

	insertSetStmt, err = db.Prepare("INSERT INTO sets (name) VALUES (?)")
	if err != nil {
		log.Fatal(err)
	}

	insertPhotoStmt, err = db.Prepare(`
	INSERT INTO photos (
	aperture, camera, exposure_comp, exposure_time, flash, focal_length,
	focal_length_35, height, iso, lat, lens, lng, path, set_id, size, taken_at,
	width
	)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		log.Fatal(err)
	}
}

func Scan(paths ...string) {
	setupDatabase()
	defer db.Close()
	defer selectSetStmt.Close()
	defer selectPhotoStmt.Close()
	defer insertSetStmt.Close()
	defer insertPhotoStmt.Close()

	for _, path := range paths {
		filepath.Walk(path, walkPath)
	}

	updatePhotoSiblings()
	updateSets()
}
