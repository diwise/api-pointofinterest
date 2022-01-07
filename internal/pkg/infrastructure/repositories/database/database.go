package database

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/diwise/api-pointofinterest/internal/pkg/domain"
)

const (
	SundsvallAnlaggningPrefix string = "se:sundsvall:facilities:"
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
	GetBeachFromID(id string) (*domain.Beach, error)
	GetAllBeaches() ([]domain.Beach, error)
	UpdateWaterTemperatureFromDeviceID(device string, temp float64, observedAt time.Time) (string, error)

	GetTrailFromID(id string) (*domain.ExerciseTrail, error)
	GetAllTrails() ([]domain.ExerciseTrail, error)
	UpdateTrailLastPreparationTime(trailID string, dateLastPreparation time.Time) error
}

//NewDatabaseConnection does not open a new connection ...
func NewDatabaseConnection(sourceURL, apiKey string, logger zerolog.Logger) (Datastore, error) {
	if sourceURL == "" || apiKey == "" {
		return nil, fmt.Errorf("all environment variables must be set")
	}

	logger.Info().Msgf("loading data from %s ...", sourceURL)

	req, err := http.NewRequest("GET", sourceURL+"/list", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("apikey", apiKey)

	resp, err := http.DefaultClient.Do(req)
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
				beach, err := parsePublishedBeach(logger, feature)
				if err != nil {
					logger.Error().Err(err).Msg("failed to parse strandbad")
					continue
				}

				db.beaches = append(db.beaches, *beach)
			} else if feature.Properties.Type == "Motionsspår" || feature.Properties.Type == "Skidspår" {
				exerciseTrail, err := parsePublishedExerciseTrail(logger, feature)
				if err != nil {
					logger.Error().Err(err).Msg("failed to parse motionsspår")
					continue
				}

				exerciseTrail.Source = fmt.Sprintf("%s/get/%d", sourceURL, feature.ID)

				db.trails = append(db.trails, *exerciseTrail)
			}
		}
	}

	return db, nil
}

func parsePublishedBeach(log zerolog.Logger, feature Feature) (*domain.Beach, error) {
	log.Info().Msgf("found published beach %d %s\n", feature.ID, feature.Properties.Name)

	beach := &domain.Beach{
		ID:          fmt.Sprintf("%s%d", SundsvallAnlaggningPrefix, feature.ID),
		Name:        feature.Properties.Name,
		Description: "",
	}

	var timeFormat string = "2006-01-02 15:04:05"

	if feature.Properties.Created != nil {
		created, err := time.Parse(timeFormat, *feature.Properties.Created)
		if err == nil {
			beach.DateCreated = created.UTC()
		}
	}

	if feature.Properties.Updated != nil {
		modified, err := time.Parse(timeFormat, *feature.Properties.Updated)
		if err == nil {
			beach.DateModified = modified.UTC()
		}
	}

	err := json.Unmarshal(feature.Geometry.Coordinates, &beach.Geometry.Lines)
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
			beach.Description = string(field.Value[1 : len(field.Value)-1])
		} else if field.ID == 230 {
			sensor := "se:servanet:lora:" + string(field.Value[1:len(field.Value)-1])
			beach.SensorID = &sensor
			log.Info().Msgf("assigning sensor %s to beach %s", sensor, beach.ID)
		}
	}

	if ref, ok := seeAlsoRefs[feature.ID]; ok {
		if len(ref.nuts) > 0 {
			beach.NUTSCode = &ref.nuts
		}

		if len(ref.wikidata) > 0 {
			beach.WikidataID = &ref.wikidata
		}
	}

	return beach, nil
}

func parsePublishedExerciseTrail(log zerolog.Logger, feature Feature) (*domain.ExerciseTrail, error) {
	log.Info().Msgf("found published exercise trail %d %s\n", feature.ID, feature.Properties.Name)

	trail := &domain.ExerciseTrail{
		ID:          fmt.Sprintf("%s%d", SundsvallAnlaggningPrefix, feature.ID),
		Name:        feature.Properties.Name,
		Description: "",
	}

	var timeFormat string = "2006-01-02 15:04:05"

	if feature.Properties.Created != nil {
		created, err := time.Parse(timeFormat, *feature.Properties.Created)
		if err == nil {
			trail.DateCreated = created.UTC()
		}
	}

	if feature.Properties.Updated != nil {
		modified, err := time.Parse(timeFormat, *feature.Properties.Updated)
		if err == nil {
			trail.DateModified = modified.UTC()
		}
	}

	err := json.Unmarshal(feature.Geometry.Coordinates, &trail.Geometry.Lines)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal geometry %s: %s", string(feature.Geometry.Coordinates), err.Error())
	}

	fields := []FeaturePropField{}
	err = json.Unmarshal(feature.Properties.Fields, &fields)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal property fields %s: %s", string(feature.Properties.Fields), err.Error())
	}

	for _, field := range fields {
		if field.ID == 99 {
			length, _ := strconv.ParseInt(string(field.Value[0:len(field.Value)]), 10, 64)
			trail.Length = float64(length) / 1000.0
		} else if field.ID == 102 {
			isOpen := string(field.Value[1 : len(field.Value)-1])
			openStatus := map[string]string{"Ja": "open", "Nej": "closed"}
			trail.Status = openStatus[isOpen]
		} else if field.ID == 110 {
			trail.Description = string(field.Value[1 : len(field.Value)-1])
		}
	}

	return trail, nil
}

type extraInfo struct {
	nuts     string
	wikidata string
	sensorID string
}

var seeAlsoRefs map[int64]extraInfo = map[int64]extraInfo{
	// Slädaviken
	283: {nuts: "SE0712281000003473", sensorID: "sk-elt-temp-21", wikidata: "Q10671745"},
	// Hartungviken
	284: {nuts: "SE0712281000003472", sensorID: "sk-elt-temp-28", wikidata: "Q680645"},
	// Tranviken
	295: {nuts: "SE0712281000003474", sensorID: "sk-elt-temp-22", wikidata: "Q106657132"},
	// Bänkåsviken
	315: {nuts: "SE0712281000003471", sensorID: "sk-elt-temp-26", wikidata: "Q106657054"},
	// Stekpannan, Hornsjön
	322: {nuts: "SE0712281000003478", sensorID: "sk-elt-temp-17", wikidata: "Q106710721"},
	// Dyket
	323: {nuts: "SE0712281000003477", sensorID: "sk-elt-temp-02", wikidata: "Q106710719"},
	// Fläsian, Nord
	337: {nuts: "SE0712281000003450", sensorID: "sk-elt-temp-25"},
	// Sodom
	357: {nuts: "SE0712281000003479", sensorID: "sk-elt-temp-27", wikidata: "Q106710722"},
	// Rännö
	414: {nuts: "SE0712281000003464", sensorID: "sk-elt-temp-08", wikidata: "Q106710690"},
	// Lucksta
	421: {nuts: "SE0712281000003461", sensorID: "sk-elt-temp-10", wikidata: "Q106710684"},
	// Norrhassel
	430: {nuts: "SE0712281000003462", sensorID: "sk-elt-temp-13", wikidata: "Q106710685"},
	// Viggesand
	442: {nuts: "SE0712281000003469", sensorID: "sk-elt-temp-12", wikidata: "Q106710700"},
	// Räveln
	456: {nuts: "SE0712281000003468", sensorID: "sk-elt-temp-19", wikidata: "Q106710698"},
	// Segersjön
	469: {nuts: "SE0712281000003452", sensorID: "sk-elt-temp-09", wikidata: "Q106710670"},
	// Vången
	488: {nuts: "SE0712281000003470", sensorID: "sk-elt-temp-16", wikidata: "Q106710701"},
	// Edeforsens badplats
	495: {nuts: "SE0712281000003467", sensorID: "sk-elt-temp-04", wikidata: "Q106710696"},
	// Pallviken
	513: {nuts: "SE0712281000003463", sensorID: "sk-elt-temp-11", wikidata: "Q106710688"},
	// Östtjärn
	526: {nuts: "SE0712281000003466", sensorID: "sk-elt-temp-18", wikidata: "Q106710694"},
	// Bergafjärden
	553: {nuts: "SE0712281000003475", sensorID: "sk-elt-temp-24", wikidata: "Q16498519"},
	// Brudsjön
	560: {nuts: "SE0712281000003455", sensorID: "sk-elt-temp-03", wikidata: "Q106710675"},
	// Sandnäset
	656: {nuts: "SE0712281000003459", sensorID: "sk-elt-temp-14", wikidata: "Q106710678"},
	657: {sensorID: "sk-elt-temp-07"}, // Abborrviken, Sidsjön
	// Västbyn
	658: {nuts: "SE0712281000003460", sensorID: "sk-elt-temp-15", wikidata: "Q106710681"},
	// Väster-Lövsjön
	659: {nuts: "SE0712281000003453", sensorID: "sk-elt-temp-05", wikidata: "Q106710672"},
	// Sidsjöns hundbad
	660: {nuts: "SE0712281000004229", sensorID: "sk-elt-temp-01"},
	// Kävstabadet, Indal
	897: {nuts: "SE0712281000003456", wikidata: "Q106710677"},
	// Bredsand
	1234: {nuts: "SE0712281000003476", sensorID: "sk-elt-temp-23", wikidata: "Q106710717"},
	// Bjässjön
	1618: {nuts: "SE0712281000003454", sensorID: "sk-elt-temp-06", wikidata: "Q106947945"},
	// Fläsian, Syd
	1631: {nuts: "SE0712281000003480", sensorID: "sk-elt-temp-20"},
}

type myDB struct {
	beaches []domain.Beach
	trails  []domain.ExerciseTrail
}

func (db *myDB) GetAllBeaches() ([]domain.Beach, error) {
	return db.beaches, nil
}

func (db *myDB) GetAllTrails() ([]domain.ExerciseTrail, error) {
	return db.trails, nil
}

func (db *myDB) GetBeachFromID(id string) (*domain.Beach, error) {
	for _, poi := range db.beaches {
		if strings.Compare(poi.ID, id) == 0 {
			return &poi, nil
		}
	}
	return nil, errors.New("not found")
}

func (db *myDB) GetTrailFromID(id string) (*domain.ExerciseTrail, error) {
	for _, trail := range db.trails {
		if strings.Compare(trail.ID, id) == 0 {
			return &trail, nil
		}
	}
	return nil, errors.New("not found")
}

func (db *myDB) UpdateTrailLastPreparationTime(trailID string, dateLastPreparation time.Time) error {
	for idx, trail := range db.trails {
		if strings.Compare(trail.ID, trailID) == 0 {
			if trail.DateLastPrepared.After(dateLastPreparation) {
				return fmt.Errorf("last preparation date may not move backwards")
			}

			db.trails[idx].DateLastPrepared = dateLastPreparation

			return nil
		}
	}

	return errors.New("not found")
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

	return "", fmt.Errorf("no beach found matching sensor ID %s", device)
}
