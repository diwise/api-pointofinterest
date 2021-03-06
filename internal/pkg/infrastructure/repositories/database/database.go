package database

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/diwise/api-pointofinterest/internal/pkg/domain"
	"github.com/diwise/api-pointofinterest/internal/pkg/infrastructure/logging"
)

const (
	SundsvallAnlaggningPrefix string = "se:sundsvall:anlaggning:"
)

type FeatureGeom struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
}

type FeaturePropField struct {
	ID    int64           `json:"id"`
	Value json.RawMessage `json:"value"`
}

type FeatureProps struct {
	Name      string          `json:"name"`
	Type      string          `json:"type"`
	Published bool            `json:"published"`
	Fields    json.RawMessage `json:"fields"`
	Created   *string         `json:"created,omitempty"`
	Updated   *string         `json:"updated,omitempty"`
}

type Feature struct {
	ID         int64        `json:"id"`
	Properties FeatureProps `json:"properties"`
	Geometry   FeatureGeom  `json:"geometry"`
}

type FeatureCollection struct {
	Type     string    `json:"type"`
	Features []Feature `json:"features"`
}

//Datastore is an interface that abstracts away the database implementation
type Datastore interface {
	GetFromID(id string) (*domain.POI, error)
	GetAllFromType(typ string) ([]domain.POI, error)

	UpdateWaterTemperatureFromDeviceID(device string, temp float64, observedAt time.Time) (string, error)
}

//NewDatabaseConnection does not open a new connection ...
func NewDatabaseConnection(sourceURL string, log logging.Logger) (Datastore, error) {
	log.Infof("Loading data from %s ...", sourceURL)
	resp, err := http.Get(sourceURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("loading data from %s failed with status %d", sourceURL, resp.StatusCode)
	}

	featureCollection := &FeatureCollection{}
	body, _ := io.ReadAll(resp.Body)
	err = json.Unmarshal(body, featureCollection)

	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal response from %s. (%s)", sourceURL, err.Error())
	}

	db := &myDB{}

	for _, feature := range featureCollection.Features {
		if feature.Properties.Published {
			if feature.Properties.Type == "Strandbad" {
				log.Infof("Hittade publicerad badplats %d %s\n", feature.ID, feature.Properties.Name)

				poi := &domain.POI{
					ID:          fmt.Sprintf("%s%d", SundsvallAnlaggningPrefix, feature.ID),
					Name:        feature.Properties.Name,
					Description: "",
				}

				var timeFormat string = "2006-01-02 15:04:05"

				if feature.Properties.Created != nil {
					created, err := time.Parse(timeFormat, *feature.Properties.Created)
					if err == nil {
						poi.DateCreated = created.UTC()
					}
				}

				if feature.Properties.Updated != nil {
					modified, err := time.Parse(timeFormat, *feature.Properties.Updated)
					if err == nil {
						poi.DateModified = modified.UTC()
					}
				}

				err = json.Unmarshal(feature.Geometry.Coordinates, &poi.Geometry.Lines)
				if err != nil {
					return nil, fmt.Errorf("failed to unmarshal geometry %s: %s", string(feature.Geometry.Coordinates), err.Error())
				}

				fields := []FeaturePropField{}
				err = json.Unmarshal(feature.Properties.Fields, &fields)
				if err != nil {
					return nil, fmt.Errorf("failed to unmarshal property fields %s: %s", string(feature.Properties.Fields), err.Error())
				}

				for _, field := range fields {
					if field.ID == 1 {
						poi.Description = string(field.Value[1 : len(field.Value)-1])
					} else if field.ID == 230 {
						sensor := "se:servanet:lora:" + string(field.Value[1:len(field.Value)-1])
						poi.SensorID = &sensor
						log.Infof("assigning sensor %s to poi %s", sensor, poi.ID)
					}
				}

				if ref, ok := seeAlsoRefs[feature.ID]; ok {
					if len(ref.nuts) > 0 {
						poi.NUTSCode = &ref.nuts
					}

					if len(ref.wikidata) > 0 {
						poi.WikidataID = &ref.wikidata
					}
				}

				db.beaches = append(db.beaches, *poi)
			}
		}
	}

	return db, nil
}

func convertSWEREFtoWGS84(x, y float64) (float64, float64) {

	//Code adapted from
	//https://github.com/bjornsallarp/MightyLittleGeodesy/blob/master/MightyLittleGeodesy/Classes/GaussKreuger.cs

	var axis float64 = 6378137.0                 // GRS 80.
	var flattening float64 = 1.0 / 298.257222101 // GRS 80.

	var centralMeridian float64 = 15.00
	var scale float64 = 0.9996
	var falseNorthing float64 = 0.0
	var falseEasting float64 = 500000.0

	e2 := flattening * (2.0 - flattening)
	n := flattening / (2.0 - flattening)

	aRoof := axis / (1.0 + n) * (1.0 + n*n/4.0 + n*n*n*n/64.0)
	delta1 := n/2.0 - 2.0*n*n/3.0 + 37.0*n*n*n/96.0 - n*n*n*n/360.0
	delta2 := n*n/48.0 + n*n*n/15.0 - 437.0*n*n*n*n/1440.0
	delta3 := 17.0*n*n*n/480.0 - 37*n*n*n*n/840.0
	delta4 := 4397.0 * n * n * n * n / 161280.0

	Astar := e2 + e2*e2 + e2*e2*e2 + e2*e2*e2*e2
	Bstar := -(7.0*e2*e2 + 17.0*e2*e2*e2 + 30.0*e2*e2*e2*e2) / 6.0
	Cstar := (224.0*e2*e2*e2 + 889.0*e2*e2*e2*e2) / 120.0
	Dstar := -(4279.0 * e2 * e2 * e2 * e2) / 1260.0

	// Convert.
	degToRad := math.Pi / 180
	lambdaZero := centralMeridian * degToRad
	xi := (x - falseNorthing) / (scale * aRoof)
	eta := (y - falseEasting) / (scale * aRoof)
	xiPrim := xi -
		delta1*math.Sin(2.0*xi)*math.Cosh(2.0*eta) -
		delta2*math.Sin(4.0*xi)*math.Cosh(4.0*eta) -
		delta3*math.Sin(6.0*xi)*math.Cosh(6.0*eta) -
		delta4*math.Sin(8.0*xi)*math.Cosh(8.0*eta)
	etaPrim := eta -
		delta1*math.Cos(2.0*xi)*math.Sinh(2.0*eta) -
		delta2*math.Cos(4.0*xi)*math.Sinh(4.0*eta) -
		delta3*math.Cos(6.0*xi)*math.Sinh(6.0*eta) -
		delta4*math.Cos(8.0*xi)*math.Sinh(8.0*eta)

	phiStar := math.Asin(math.Sin(xiPrim) / math.Cosh(etaPrim))
	deltaLambda := math.Atan(math.Sinh(etaPrim) / math.Cos(xiPrim))

	lonRadian := lambdaZero + deltaLambda
	latRadian := phiStar + math.Sin(phiStar)*math.Cos(phiStar)*
		(Astar+
			Bstar*math.Pow(math.Sin(phiStar), 2)+
			Cstar*math.Pow(math.Sin(phiStar), 4)+
			Dstar*math.Pow(math.Sin(phiStar), 6))

	lat := latRadian * 180.0 / math.Pi
	lon := lonRadian * 180.0 / math.Pi

	return lon, lat
}

type extraInfo struct {
	nuts     string
	wikidata string
	sensorID string
}

var seeAlsoRefs map[int64]extraInfo = map[int64]extraInfo{
	// Sl??daviken
	283: {nuts: "SE0712281000003473", sensorID: "sk-elt-temp-21", wikidata: "Q10671745"},
	// Hartungviken
	284: {nuts: "SE0712281000003472", sensorID: "sk-elt-temp-28", wikidata: "Q680645"},
	// Tranviken
	295: {nuts: "SE0712281000003474", sensorID: "sk-elt-temp-22", wikidata: "Q106657132"},
	// B??nk??sviken
	315: {nuts: "SE0712281000003471", sensorID: "sk-elt-temp-26", wikidata: "Q106657054"},
	// Stekpannan, Hornsj??n
	322: {nuts: "SE0712281000003478", sensorID: "sk-elt-temp-17", wikidata: "Q106710721"},
	// Dyket
	323: {nuts: "SE0712281000003477", sensorID: "sk-elt-temp-02", wikidata: "Q106710719"},
	// Fl??sian, Nord
	337: {nuts: "SE0712281000003450", sensorID: "sk-elt-temp-25"},
	// Sodom
	357: {nuts: "SE0712281000003479", sensorID: "sk-elt-temp-27", wikidata: "Q106710722"},
	// R??nn??
	414: {nuts: "SE0712281000003464", sensorID: "sk-elt-temp-08", wikidata: "Q106710690"},
	// Lucksta
	421: {nuts: "SE0712281000003461", sensorID: "sk-elt-temp-10", wikidata: "Q106710684"},
	// Norrhassel
	430: {nuts: "SE0712281000003462", sensorID: "sk-elt-temp-13", wikidata: "Q106710685"},
	// Viggesand
	442: {nuts: "SE0712281000003469", sensorID: "sk-elt-temp-12", wikidata: "Q106710700"},
	// R??veln
	456: {nuts: "SE0712281000003468", sensorID: "sk-elt-temp-19", wikidata: "Q106710698"},
	// Segersj??n
	469: {nuts: "SE0712281000003452", sensorID: "sk-elt-temp-09", wikidata: "Q106710670"},
	// V??ngen
	488: {nuts: "SE0712281000003470", sensorID: "sk-elt-temp-16", wikidata: "Q106710701"},
	// Edeforsens badplats
	495: {nuts: "SE0712281000003467", sensorID: "sk-elt-temp-04", wikidata: "Q106710696"},
	// Pallviken
	513: {nuts: "SE0712281000003463", sensorID: "sk-elt-temp-11", wikidata: "Q106710688"},
	// ??sttj??rn
	526: {nuts: "SE0712281000003466", sensorID: "sk-elt-temp-18", wikidata: "Q106710694"},
	// Bergafj??rden
	553: {nuts: "SE0712281000003475", sensorID: "sk-elt-temp-24", wikidata: "Q16498519"},
	// Brudsj??n
	560: {nuts: "SE0712281000003455", sensorID: "sk-elt-temp-03", wikidata: "Q106710675"},
	// Sandn??set
	656: {nuts: "SE0712281000003459", sensorID: "sk-elt-temp-14", wikidata: "Q106710678"},
	657: {sensorID: "sk-elt-temp-07"}, // Abborrviken, Sidsj??n
	// V??stbyn
	658: {nuts: "SE0712281000003460", sensorID: "sk-elt-temp-15", wikidata: "Q106710681"},
	// V??ster-L??vsj??n
	659: {nuts: "SE0712281000003453", sensorID: "sk-elt-temp-05", wikidata: "Q106710672"},
	// Sidsj??ns hundbad
	660: {nuts: "SE0712281000004229", sensorID: "sk-elt-temp-01"},
	// K??vstabadet, Indal
	897: {nuts: "SE0712281000003456", wikidata: "Q106710677"},
	// Bredsand
	1234: {nuts: "SE0712281000003476", sensorID: "sk-elt-temp-23", wikidata: "Q106710717"},
	// Bj??ssj??n
	1618: {nuts: "SE0712281000003454", sensorID: "sk-elt-temp-06", wikidata: "Q106947945"},
	// Fl??sian, Syd
	1631: {nuts: "SE0712281000003480", sensorID: "sk-elt-temp-20"},
}

func lookupTempSensorFromBeachID(beach int64) *string {
	if sensor, ok := seeAlsoRefs[beach]; ok {
		prefixedSensor := fmt.Sprintf("se:servanet:lora:%s", sensor.sensorID)
		return &prefixedSensor
	}
	return nil
}

type myDB struct {
	beaches []domain.POI
}

func (db *myDB) GetFromID(id string) (*domain.POI, error) {
	for _, poi := range db.beaches {
		if strings.Compare(poi.ID, id) == 0 {
			return &poi, nil
		}
	}
	return nil, errors.New("not found")
}

func (db *myDB) GetAllFromType(typ string) ([]domain.POI, error) {
	return db.beaches, nil
}

func (db *myDB) UpdateWaterTemperatureFromDeviceID(device string, temp float64, observedAt time.Time) (string, error) {

	for idx, poi := range db.beaches {
		if poi.SensorID != nil && *poi.SensorID == device {
			if observedAt.After(poi.DateModified) {
				db.beaches[idx].WaterTemperature = &temp
				db.beaches[idx].DateModified = time.Now().UTC()
				return poi.ID, nil
			} else {
				return poi.ID, fmt.Errorf("ignored temperature update that predates datemodified of %s", poi.ID)
			}
		}
	}

	return "", fmt.Errorf("no POI found matching sensor ID %s", device)
}
