package domain

import "time"

type MultiPolygon struct {
	Lines [][][][]float64
}

//POI contains a point of interest
type POI struct {
	ID           string
	Name         string
	Description  string
	Geometry     MultiPolygon
	WikidataID   *string
	SensorID     *string
	DateCreated  time.Time
	DateModified time.Time
}
