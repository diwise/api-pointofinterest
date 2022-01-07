package domain

import "time"

type LineString struct {
	Lines [][]float64
}

type MultiPolygon struct {
	Lines [][][][]float64
}

//Beach contains a point of interest of type Beach
type Beach struct {
	ID               string
	Name             string
	Description      string
	Geometry         MultiPolygon
	WikidataID       *string
	NUTSCode         *string
	SensorID         *string
	WaterTemperature *float64
	DateCreated      time.Time
	DateModified     time.Time
}

type ExerciseTrail struct {
	ID               string
	Name             string
	Description      string
	Length           float64
	Geometry         LineString
	Status           string
	DateCreated      time.Time
	DateModified     time.Time
	DateLastPrepared time.Time
	Source           string
}
